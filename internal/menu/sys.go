// sys.go: system-tools menu sub-flows (options 11-16).
//
// Stubs; implementations land per the v2.2 priority-4 task.

package menu

import "bufio"

func runSpeedTest(r *bufio.Reader) error { return notImplemented(r, "11 SPEED TEST") }
func runBWChart(r *bufio.Reader) error   { return notImplemented(r, "12 BW chart") }
func runOptimize(r *bufio.Reader) error  { return notImplemented(r, "13 เพิ่มประสิทธิภาพ") }
func runBackup(r *bufio.Reader) error    { return notImplemented(r, "14 BACKUP") }
func runLimiter(r *bufio.Reader) error   { return notImplemented(r, "15 LIMITER") }
func runVPSInfo(r *bufio.Reader) error   { return notImplemented(r, "16 ข้อมูล VPS") }
