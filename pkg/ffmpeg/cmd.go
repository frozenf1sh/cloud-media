package ffmpeg

import "strings"

// FormatCommand 格式化 FFmpeg 命令用于日志输出
func FormatCommand(args []string) string {
	var sb strings.Builder
	for i, arg := range args {
		if i > 0 {
			sb.WriteString(" ")
		}
		// 包含空格的参数需要引号
		if strings.ContainsAny(arg, " \\\"'") {
			sb.WriteString("'")
			sb.WriteString(strings.ReplaceAll(arg, "'", "\\'"))
			sb.WriteString("'")
		} else {
			sb.WriteString(arg)
		}
	}
	return sb.String()
}
