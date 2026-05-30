package gui

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"unicode/utf16"
)

// startSplash shows a "please wait" indicator during the one-time Electron
// install and returns a function that dismisses it. It is a package variable so
// tests can swap it out. See defaultStartSplash for the real behavior.
//
// (The variable itself is declared in install.go alongside the other seams.)

// defaultStartSplash pops a native WinForms window on Windows (via PowerShell)
// and returns a closer that kills it. On other OSes — or if PowerShell can't be
// started — it degrades to a single stderr line with the same signature.
func defaultStartSplash(message string) func() {
	if runtime.GOOS != "windows" {
		fmt.Fprintln(os.Stderr, message)
		return func() {}
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-EncodedCommand", encodePowershell(splashScript(message)))
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, message)
		return func() {}
	}
	return func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill() // closes the window when the install finishes
		}
	}
}

// splashScript builds a small WinForms script: a centered, top-most dialog with
// the message, a "(pode levar alguns minutos)" hint, and an indeterminate
// marquee progress bar. ShowDialog blocks until the process is killed.
func splashScript(message string) string {
	msg := strings.ReplaceAll(message, "'", "''") // escape for PS single-quoted string
	return `Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$f = New-Object System.Windows.Forms.Form
$f.Text = 'GoDynamo'
$f.Width = 380
$f.Height = 150
$f.StartPosition = 'CenterScreen'
$f.FormBorderStyle = 'FixedDialog'
$f.ControlBox = $false
$f.TopMost = $true
$l = New-Object System.Windows.Forms.Label
$l.Text = '` + msg + `'
$l.AutoSize = $false
$l.SetBounds(20, 20, 340, 30)
$s = New-Object System.Windows.Forms.Label
$s.Text = '(pode levar alguns minutos)'
$s.AutoSize = $false
$s.SetBounds(20, 50, 340, 20)
$p = New-Object System.Windows.Forms.ProgressBar
$p.Style = 'Marquee'
$p.MarqueeAnimationSpeed = 30
$p.SetBounds(20, 80, 340, 20)
$f.Controls.Add($l)
$f.Controls.Add($s)
$f.Controls.Add($p)
[void]$f.ShowDialog()`
}

// encodePowershell encodes a script as UTF-16LE then base64 for PowerShell's
// -EncodedCommand. This sidesteps console code-page and quoting problems with
// the accented message ("instalação") and the ellipsis.
func encodePowershell(script string) string {
	u := utf16.Encode([]rune(script))
	buf := make([]byte, 0, len(u)*2)
	for _, r := range u {
		buf = append(buf, byte(r), byte(r>>8)) // little-endian
	}
	return base64.StdEncoding.EncodeToString(buf)
}
