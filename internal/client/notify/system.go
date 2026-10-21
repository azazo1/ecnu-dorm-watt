// Package notify 发送带图标和点击动作的跨平台系统通知.
package notify

import (
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/gen2brain/beeep"
)

// System 使用操作系统通知中心发送通知.
type System struct {
	// URL 是点击通知后打开的本地 Web 地址.
	URL string
	// IconPath 是通知图标路径.
	IconPath string

	warnOnce sync.Once
}

// New 创建系统通知器.
func New(webURL string) (*System, error) {
	icon, err := EnsureIcon()
	if err != nil {
		return nil, err
	}
	return &System{URL: strings.TrimSpace(webURL), IconPath: icon}, nil
}

// Notify 发送一条桌面通知. 点击通知会尝试打开本地 Web.
func (s *System) Notify(title, body string) error {
	if s.URL == "" {
		s.URL = "http://127.0.0.1:20532/"
	}
	switch runtime.GOOS {
	case "darwin":
		return s.notifyDarwin(title, body)
	case "windows":
		return s.notifyWindows(title, body)
	case "linux":
		return s.notifyLinux(title, body)
	default:
		return beeep.Notify(title, body, s.IconPath)
	}
}

func (s *System) notifyDarwin(title, body string) error {
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		cmd := exec.Command(path,
			"-title", title,
			"-message", body,
			"-appIcon", s.IconPath,
			"-execute", "open "+shellQuote(s.URL),
		)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	if err := s.notifyDarwinJXA(title, body); err == nil {
		return nil
	}
	s.warnOnce.Do(func() {
		slog.Warn("macOS 原生通知不可用, 退回普通通知; 点击动作与图标可能缺失")
	})
	script := fmt.Sprintf("display notification %s with title %s", appleString(body), appleString(title))
	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		return beeep.Notify(title, body, s.IconPath)
	}
	return nil
}

// notifyDarwinJXA 使用 osascript 的 ObjC 桥注册通知代理.
//
// 每个通知单独启动一个进程, 进程在点击, 关闭或 120 秒后退出.
// 这样不需要安装 terminal-notifier, 同时保留点击打开本地 Web 的能力.
func (s *System) notifyDarwinJXA(title, body string) error {
	script := fmt.Sprintf(`ObjC.import("Cocoa");

ObjC.registerSubclass({
  name: "DWNotification",
  methods: {
    "userNotificationCenter:shouldPresentNotification:": {
      types: ["bool", ["id", "id"]],
      implementation: function () { return true; }
    },
    "userNotificationCenter:didActivateNotification:": {
      types: ["void", ["id", "id"]],
      implementation: function () {
        $.NSWorkspace.sharedWorkspace.openURL($.NSURL.URLWithString(%s));
        $.NSApp.terminate(null);
      }
    },
    "userNotificationCenter:didDismissNotification:": {
      types: ["void", ["id", "id"]],
      implementation: function () { $.NSApp.terminate(null); }
    },
    "timeout:": {
      types: ["void", ["id"]],
      implementation: function () { $.NSApp.terminate(null); }
    }
  }
});

var delegate = $.DWNotification.alloc.init;
var center = $.NSUserNotificationCenter.defaultUserNotificationCenter;
center.delegate = delegate;

var notification = $.NSUserNotification.alloc.init;
notification.title = %s;
notification.informativeText = %s;
notification.soundName = $.NSUserNotificationDefaultSoundName;
notification.contentImage = $.NSImage.alloc.initWithContentsOfFile(%s);
center.deliverNotification(notification);

$.NSApplication.sharedApplication.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
$.NSTimer.scheduledTimerWithTimeIntervalTargetSelectorUserInfoRepeats(120, delegate, "timeout:", null, false);
$.NSApplication.sharedApplication.run();
`, jsString(s.URL), jsString(title), jsString(body), jsString(s.IconPath))
	cmd := exec.Command("osascript", "-l", "JavaScript", "-e", script)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			slog.Debug("macOS JXA 通知进程退出", "error", err)
		}
	}()
	return nil
}

func (s *System) notifyLinux(title, body string) error {
	path, err := exec.LookPath("notify-send")
	if err != nil {
		return beeep.Notify(title, body, s.IconPath)
	}
	go func() {
		args := []string{
			"--app-name=DormWatt",
			"--icon=" + s.IconPath,
			"--action=default=打开 DormWatt",
			"--wait",
			title,
			body,
		}
		out, err := exec.Command(path, args...).Output()
		if err == nil {
			if strings.TrimSpace(string(out)) == "default" {
				_ = openURL(s.URL)
			}
			return
		}
		// 老版本 notify-send 不支持 action/wait, 退回普通通知.
		_ = exec.Command(path, "--app-name=DormWatt", "--icon="+s.IconPath, title, body).Run()
	}()
	return nil
}

func (s *System) notifyWindows(title, body string) error {
	path, err := exec.LookPath("powershell")
	if err != nil {
		path, err = exec.LookPath("powershell.exe")
	}
	if err != nil {
		return beeep.Notify(title, body, s.IconPath)
	}
	toast := `<toast activationType="protocol" launch="` + xmlEscape(s.URL) + `">` +
		`<visual><binding template="ToastGeneric">` +
		`<text>` + xmlEscape(title) + `</text>` +
		`<text>` + xmlEscape(body) + `</text>` +
		`<image placement="appLogoOverride" src="` + xmlEscape(fileURL(s.IconPath)) + `"/>` +
		`</binding></visual></toast>`
	script := `$ErrorActionPreference = 'Stop'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml('` + strings.ReplaceAll(toast, "'", "''") + `')
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('DormWatt').Show($toast)
`
	encoded := encodePowerShell(script)
	cmd := exec.Command(path, "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	if err := cmd.Run(); err != nil {
		return beeep.Notify(title, body, s.IconPath)
	}
	return nil
}

func openURL(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func appleString(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(s) + `"`
}

func jsString(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
		"\n", `\n`,
		"\r", `\r`,
		"\t", `\t`,
	)
	return `"` + replacer.Replace(s) + `"`
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func xmlEscape(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(s)
}

func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

func encodePowerShell(script string) string {
	runes := utf16.Encode([]rune(script))
	raw := make([]byte, 0, len(runes)*2)
	for _, r := range runes {
		raw = append(raw, byte(r), byte(r>>8))
	}
	return base64.StdEncoding.EncodeToString(raw)
}
