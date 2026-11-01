// No trouble

package std

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// writeLines logs n records of the form "line<counter>\n" through the writer.
func writeLines(t *testing.T, w *RollingLog, from, to int) {
	t.Helper()
	for i := from; i <= to; i++ {
		if _, err := fmt.Fprintf(w, "line%d\n", i); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// lineNumbers extracts the sequence numbers still present in the log, and fails
// on anything that is not a complete line, which is how a torn head shows up.
func lineNumbers(t *testing.T, content string) []int {
	t.Helper()
	var nums []int
	re := regexp.MustCompile(`^line([0-9]+)$`)
	for _, l := range strings.Split(content, "\n") {
		if l == "" {
			continue
		}
		m := re.FindStringSubmatch(l)
		if m == nil {
			t.Fatalf("残缺或错位的行: %q (完整内容 %q)", l, content)
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		nums = append(nums, n)
	}
	return nums
}

func newLog(t *testing.T, limitKB int) (*RollingLog, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client.log")
	r, err := NewRollingLog(path, limitKB)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, path
}

func TestRollingLogUnlimitedKeepsAppending(t *testing.T) {
	r, path := newLog(t, 0)
	writeLines(t, r, 1, 500)

	if got := int(r.Size()); got != len(readFile(t, path)) {
		t.Fatalf("size 与文件实际长度不一致: %d vs %d", got, len(readFile(t, path)))
	}
	if !strings.HasPrefix(readFile(t, path), "line1\n") {
		t.Fatal("不限制时不应丢任何内容")
	}
	if nums := lineNumbers(t, readFile(t, path)); len(nums) != 500 {
		t.Fatalf("期望 500 行, 实际 %d", len(nums))
	}
}

func TestRollingLogStaysWithinLimit(t *testing.T) {
	// 8 KB 上限，写入远超这个量的日志
	limit := 8
	r, path := newLog(t, limit)
	writeLines(t, r, 1, 5000)

	size := len(readFile(t, path))
	if size > limit*1024 {
		t.Fatalf("文件超过上限: %d > %d", size, limit*1024)
	}
	// 压缩后文件会稳定在上限附近，而不是一直很小
	if size < limit*1024/rollingRetainDiv/2 {
		t.Fatalf("文件被压得过小, 说明窗口没有保持住: %d", size)
	}
}

func TestRollingLogKeepsNewestInOrder(t *testing.T) {
	r, path := newLog(t, 8)
	writeLines(t, r, 1, 5000)

	nums := lineNumbers(t, readFile(t, path))
	if len(nums) == 0 {
		t.Fatal("压缩后不应把文件清空")
	}
	if !sort.IntsAreSorted(nums) {
		t.Fatalf("剩下的行必须按时间顺序排列, 前几个: %v", nums[:min(len(nums), 5)])
	}
	if nums[len(nums)-1] != 5000 {
		t.Fatalf("最新一条必须在文件里, 实际末尾是 %d", nums[len(nums)-1])
	}
	// 只丢最旧的：留下的编号必须是连续的一段且以最新结尾
	if nums[len(nums)-1]-nums[0] != len(nums)-1 {
		t.Fatalf("保留的内容应当是连续的最新片段: %d..%d 共 %d 行", nums[0], nums[len(nums)-1], len(nums))
	}
}

func TestRollingLogTrimsExistingOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(f, strings.Repeat("ancient-line\n", 4000))
	f.Close()

	before := int64(len("ancient-line\n") * 4000)
	r, err := NewRollingLog(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Size() > 8*1024 {
		t.Fatalf("打开时就该把历史文件压到上限内, 实际 %d", r.Size())
	}
	if r.Size() >= before {
		t.Fatal("打开时没有做压缩")
	}
	// 压缩完还能继续正常写
	if _, err := fmt.Fprintf(r, "line1\n"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(readFile(t, path), "line1\n") {
		t.Fatal("打开后写入异常")
	}
}

func TestRollingLogOversizedSingleRecord(t *testing.T) {
	r, path := newLog(t, 1) // 1 KB 上限

	// 先放几行正常内容，再写一条比整个文件还大的记录
	writeLines(t, r, 1, 3)
	big := "head-noise" + strings.Repeat("x", 2048) + "\n"
	if _, err := r.Write([]byte(big)); err != nil {
		t.Fatal(err)
	}

	content := readFile(t, path)
	if len(content) > 1024 {
		t.Fatalf("单条超限记录后仍必须守住上限, 实际 %d", len(content))
	}
	if !strings.HasSuffix(content, "x\n") {
		t.Fatal("超限记录应保留自己的尾部")
	}
	if strings.Contains(content, "line3") {
		t.Fatal("装不下时旧内容就该被顶掉")
	}
	// 之后还能继续写
	writeLines(t, r, 4, 6)
	if !strings.HasSuffix(readFile(t, path), "line6\n") {
		t.Fatal("超限写入之后应能继续正常追加")
	}
}

func TestRollingLogSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client.log")
	r, err := NewRollingLog(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	writeLines(t, r, 1, 900)
	r.Close()

	// 模拟进程重启：新句柄必须从文件末尾继续写，而不是覆盖
	r2, err := NewRollingLog(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	writeLines(t, r2, 901, 1200)

	nums := lineNumbers(t, readFile(t, path))
	if nums[len(nums)-1] != 1200 {
		t.Fatalf("重启后写入的内容应在末尾, 实际 %d", nums[len(nums)-1])
	}
	if !sort.IntsAreSorted(nums) {
		t.Fatal("重启后顺序被打乱")
	}
}

func TestRollingLogConcurrentWrites(t *testing.T) {
	r, path := newLog(t, 16)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				if _, err := fmt.Fprintf(r, "line%d\n", g*1000+i); err != nil {
					t.Errorf("write: %v", err)
				}
			}
		}(g)
	}
	wg.Wait()

	if size := len(readFile(t, path)); size > 16*1024 {
		t.Fatalf("并发写入下超过上限: %d", size)
	}
	// 每一行都必须是完整的，交错写不能撕裂记录
	if nums := lineNumbers(t, readFile(t, path)); len(nums) == 0 {
		t.Fatal("并发写入后文件为空")
	}
	sc := bufio.NewScanner(strings.NewReader(readFile(t, path)))
	sc.Split(bufio.ScanLines)
	for sc.Scan() {
		if !strings.HasPrefix(sc.Text(), "line") {
			t.Fatalf("出现被撕裂的行: %q", sc.Text())
		}
	}
}

func TestRollingLogRejectsEmptyPath(t *testing.T) {
	if _, err := NewRollingLog("", 8); err == nil {
		t.Fatal("空路径应该报错")
	}
}
