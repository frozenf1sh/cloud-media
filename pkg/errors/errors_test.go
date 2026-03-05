package errors

import (
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	err := New(CodeInvalidArgument, "test error")
	if err.Code != CodeInvalidArgument {
		t.Errorf("Expected code %v, got %v", CodeInvalidArgument, err.Code)
	}
	if err.Message != "test error" {
		t.Errorf("Expected message 'test error', got '%v'", err.Message)
	}
}

func TestNewf(t *testing.T) {
	err := Newf(CodeNotFound, "resource %s not found", "test")
	if err.Code != CodeNotFound {
		t.Errorf("Expected code %v, got %v", CodeNotFound, err.Code)
	}
	if err.Message != "resource test not found" {
		t.Errorf("Expected message 'resource test not found', got '%v'", err.Message)
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("original error")
	err := Wrap(CodeInternal, "wrapper", cause)
	if err.Code != CodeInternal {
		t.Errorf("Expected code %v, got %v", CodeInternal, err.Code)
	}
	if err.Message != "wrapper" {
		t.Errorf("Expected message 'wrapper', got '%v'", err.Message)
	}
	if err.Unwrap() != cause {
		t.Errorf("Expected cause %v, got %v", cause, err.Unwrap())
	}
}

func TestWrapf(t *testing.T) {
	cause := errors.New("original error")
	err := Wrapf(CodeTranscodeError, cause, "failed to transcode %s", "video.mp4")
	if err.Code != CodeTranscodeError {
		t.Errorf("Expected code %v, got %v", CodeTranscodeError, err.Code)
	}
	if err.Message != "failed to transcode video.mp4" {
		t.Errorf("Expected message 'failed to transcode video.mp4', got '%v'", err.Message)
	}
}

func TestError(t *testing.T) {
	err := New(CodeInvalidArgument, "test error")
	expected := "INVALID_ARGUMENT: test error"
	if err.Error() != expected {
		t.Errorf("Expected '%v', got '%v'", expected, err.Error())
	}

	cause := errors.New("cause")
	errWithCause := Wrap(CodeInternal, "test", cause)
	expectedWithCause := "INTERNAL_ERROR: test (cause: cause)"
	if errWithCause.Error() != expectedWithCause {
		t.Errorf("Expected '%v', got '%v'", expectedWithCause, errWithCause.Error())
	}
}

func TestIsAppError(t *testing.T) {
	appErr := New(CodeInvalidArgument, "test")
	otherErr := errors.New("other")

	result, ok := IsAppError(appErr)
	if !ok {
		t.Error("Expected true for AppError")
	}
	if result != appErr {
		t.Error("Expected same error")
	}

	result, ok = IsAppError(otherErr)
	if ok {
		t.Error("Expected false for non-AppError")
	}
	if result != nil {
		t.Error("Expected nil for non-AppError")
	}
}

func TestHasCode(t *testing.T) {
	err := New(CodeInvalidArgument, "test")
	if !HasCode(err, CodeInvalidArgument) {
		t.Error("Expected HasCode to return true for same code")
	}
	if HasCode(err, CodeNotFound) {
		t.Error("Expected HasCode to return false for different code")
	}

	otherErr := errors.New("other")
	if HasCode(otherErr, CodeInvalidArgument) {
		t.Error("Expected HasCode to return false for non-AppError")
	}
}

func TestHelperFunctions(t *testing.T) {
	tests := []struct {
		name string
		fn   func() *AppError
		want ErrorCode
	}{
		{
			name: "InvalidArgument",
			fn:   func() *AppError { return InvalidArgument("msg") },
			want: CodeInvalidArgument,
		},
		{
			name: "InvalidArgumentf",
			fn:   func() *AppError { return InvalidArgumentf("msg %s", "x") },
			want: CodeInvalidArgument,
		},
		{
			name: "NotFound",
			fn:   func() *AppError { return NotFound("msg") },
			want: CodeNotFound,
		},
		{
			name: "NotFoundf",
			fn:   func() *AppError { return NotFoundf("msg %s", "x") },
			want: CodeNotFound,
		},
		{
			name: "AlreadyExists",
			fn:   func() *AppError { return AlreadyExists("msg") },
			want: CodeAlreadyExists,
		},
		{
			name: "AlreadyExistsf",
			fn:   func() *AppError { return AlreadyExistsf("msg %s", "x") },
			want: CodeAlreadyExists,
		},
		{
			name: "Internal",
			fn:   func() *AppError { return Internal("msg") },
			want: CodeInternal,
		},
		{
			name: "Internalf",
			fn:   func() *AppError { return Internalf("msg %s", "x") },
			want: CodeInternal,
		},
		{
			name: "InvalidVideoFile",
			fn:   func() *AppError { return InvalidVideoFile("msg") },
			want: CodeInvalidVideoFile,
		},
		{
			name: "InvalidVideoFilef",
			fn:   func() *AppError { return InvalidVideoFilef("msg %s", "x") },
			want: CodeInvalidVideoFile,
		},
		{
			name: "FileTooLarge",
			fn:   func() *AppError { return FileTooLarge(100) },
			want: CodeFileTooLarge,
		},
		{
			name: "FileTooSmall",
			fn:   func() *AppError { return FileTooSmall(10) },
			want: CodeFileTooSmall,
		},
		{
			name: "UnsupportedFormat",
			fn:   func() *AppError { return UnsupportedFormat("mp4") },
			want: CodeUnsupportedFormat,
		},
		{
			name: "InvalidAspectRatio",
			fn:   func() *AppError { return InvalidAspectRatio("msg") },
			want: CodeInvalidAspectRatio,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err.Code != tt.want {
				t.Errorf("Expected code %v, got %v", tt.want, err.Code)
			}
		})
	}
}

func TestStorageError(t *testing.T) {
	cause := errors.New("s3 error")
	err := StorageError("failed to upload", cause)
	if err.Code != CodeStorageError {
		t.Errorf("Expected code %v, got %v", CodeStorageError, err.Code)
	}
	if err.Unwrap() != cause {
		t.Errorf("Expected cause %v, got %v", cause, err.Unwrap())
	}
}

func TestTranscodeError(t *testing.T) {
	cause := errors.New("ffmpeg error")
	err := TranscodeError("failed to transcode", cause)
	if err.Code != CodeTranscodeError {
		t.Errorf("Expected code %v, got %v", CodeTranscodeError, err.Code)
	}
	if err.Unwrap() != cause {
		t.Errorf("Expected cause %v, got %v", cause, err.Unwrap())
	}
}
