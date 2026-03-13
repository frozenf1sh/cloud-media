package ffmpeg

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
)

// ProgressCallback 进度回调函数
type ProgressCallback func(progress int, message string)

// ProgressParser FFmpeg 进度解析器
type ProgressParser struct {
	timeRegex *regexp.Regexp
}

// NewProgressParser 创建进度解析器
func NewProgressParser() *ProgressParser {
	return &ProgressParser{
		timeRegex: regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})\.(\d{2})`),
	}
}

// Parse 解析 FFmpeg 输出并调用回调
func (p *ProgressParser) Parse(stderr io.ReadCloser, duration float64, callback ProgressCallback) {
	scanner := bufio.NewScanner(stderr)

	// 跟踪最大进度，防止在多路输出时进度回退
	maxProgress := 0
	lastReportedProgress := -1

	for scanner.Scan() {
		line := scanner.Text()

		if matches := p.timeRegex.FindStringSubmatch(line); matches != nil {
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.Atoi(matches[3])
			csec, _ := strconv.Atoi(matches[4])

			currentTime := float64(hours*3600+minutes*60+seconds) + float64(csec)/100.0

			if duration > 0 {
				progress := int((currentTime / duration) * 100)
				if progress > 100 {
					progress = 100
				}

				// 只记录最大进度，防止多路输出时进度回退
				if progress > maxProgress {
					maxProgress = progress
				}

				// 只在进度变化至少 1% 时才回调，避免过于频繁
				if callback != nil && maxProgress != lastReportedProgress {
					lastReportedProgress = maxProgress
					callback(maxProgress, fmt.Sprintf("Processing: %.1f/%.1fs", currentTime, duration))
				}
			}
		}
	}
}
