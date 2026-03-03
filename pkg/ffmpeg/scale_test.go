package ffmpeg

import (
	"testing"
)

func TestScaleCalculator_Calculate(t *testing.T) {
	tests := []struct {
		name           string
		originalWidth  int
		originalHeight int
		targetSize     int
		wantWidth      int
		wantHeight     int
	}{
		{
			name:           "横屏1080p-16:9",
			originalWidth:  3840,
			originalHeight: 2160,
			targetSize:     1080,
			wantWidth:      1920,
			wantHeight:     1080,
		},
		{
			name:           "横屏720p-16:9",
			originalWidth:  1920,
			originalHeight: 1080,
			targetSize:     720,
			wantWidth:      1280,
			wantHeight:     720,
		},
		{
			name:           "横屏480p-4:3",
			originalWidth:  1440,
			originalHeight: 1080,
			targetSize:     480,
			wantWidth:      640,
			wantHeight:     480,
		},
		{
			name:           "竖屏1080p-9:16",
			originalWidth:  1080,
			originalHeight: 1920,
			targetSize:     1080,
			wantWidth:      1080,
			wantHeight:     1920,
		},
		{
			name:           "竖屏720p-9:16",
			originalWidth:  1080,
			originalHeight: 1920,
			targetSize:     720,
			wantWidth:      720,
			wantHeight:     1280,
		},
		{
			name:           "确保偶数-奇数结果",
			originalWidth:  1280,
			originalHeight: 720,
			targetSize:     480,
			wantWidth:      852,
			wantHeight:     480,
		},
	}

	calc := NewScaleCalculator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWidth, gotHeight := calc.Calculate(tt.originalWidth, tt.originalHeight, tt.targetSize)
			if gotWidth != tt.wantWidth {
				t.Errorf("Calculate() width = %v, want %v", gotWidth, tt.wantWidth)
			}
			if gotHeight != tt.wantHeight {
				t.Errorf("Calculate() height = %v, want %v", gotHeight, tt.wantHeight)
			}
			// 确保结果是偶数
			if gotWidth%2 != 0 {
				t.Errorf("Calculate() width = %v, should be even", gotWidth)
			}
			if gotHeight%2 != 0 {
				t.Errorf("Calculate() height = %v, should be even", gotHeight)
			}
		})
	}
}

func TestAspectRatioValidator_Validate(t *testing.T) {
	tests := []struct {
		name    string
		width   int
		height  int
		wantErr bool
	}{
		{
			name:    "正常16:9",
			width:   1920,
			height:  1080,
			wantErr: false,
		},
		{
			name:    "正常9:16",
			width:   1080,
			height:  1920,
			wantErr: false,
		},
		{
			name:    "正常1:1",
			width:   1080,
			height:  1080,
			wantErr: false,
		},
		{
			name:    "过宽-20:1",
			width:   4000,
			height:  200,
			wantErr: true,
		},
		{
			name:    "过高-1:20",
			width:   200,
			height:  4000,
			wantErr: true,
		},
		{
			name:    "边界-16:1",
			width:   1600,
			height:  100,
			wantErr: false,
		},
		{
			name:    "边界-1:16",
			width:   100,
			height:  1600,
			wantErr: false,
		},
	}

	validator := NewDefaultAspectRatioValidator()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.width, tt.height)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
