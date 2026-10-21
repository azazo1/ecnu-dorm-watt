package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// readLine 从输入读取一行, 去掉行尾换行与回车.
func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readLineFrom 从任意输入读取一行.
func readLineFrom(r io.Reader) (string, error) {
	return readLine(bufio.NewReader(r))
}

// confirm 询问是否继续, 默认否.
func (a *App) confirm(prompt string) (bool, error) {
	fmt.Fprintf(a.stdout, "%s [y/N]: ", prompt)
	reader := bufio.NewReader(a.stdin)
	line, err := readLine(reader)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "是":
		return true, nil
	}
	return false, nil
}

// readPasswordish 从 stdin 读取一行敏感内容. 终端下无法隐藏回显, 会提示使用管道.
func (a *App) readSecretLine(label string) (string, error) {
	if isTerminalReader(a.stdin) {
		fmt.Fprintf(a.stderr, "提示: 当前从终端读取 %s, 回显无法隐藏, 建议改用管道输入\n", label)
	}
	fmt.Fprintf(a.stderr, "%s: ", label)
	reader := bufio.NewReader(a.stdin)
	line, err := readLine(reader)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(a.stderr)
	return strings.TrimSpace(line), nil
}

// chooseOption 交互式选择一个选项, 返回下标.
func (a *App) chooseOption(prompt string, options []string) (int, error) {
	if len(options) == 0 {
		return 0, fmt.Errorf("%s 没有可选项", prompt)
	}
	fmt.Fprintf(a.stdout, "? %s\n", prompt)
	for i, opt := range options {
		fmt.Fprintf(a.stdout, "  %d) %s\n", i+1, opt)
	}
	reader := bufio.NewReader(a.stdin)
	for {
		fmt.Fprintf(a.stdout, "选择 [1-%d]: ", len(options))
		line, err := readLine(reader)
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(options) {
			fmt.Fprintf(a.stdout, "请输入 1 到 %d 之间的数字\n", len(options))
			continue
		}
		return n - 1, nil
	}
}

// readAllLines 读取最多 n 行非空内容.
func readAllLines(r io.Reader, n int) ([]string, error) {
	reader := bufio.NewReader(r)
	out := make([]string, 0, n)
	for len(out) < n {
		line, err := readLine(reader)
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	return out, nil
}
