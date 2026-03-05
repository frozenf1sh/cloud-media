package errors

import (
	"errors"
	"fmt"
)

// ErrorCode 错误码类型
type ErrorCode string

const (
	// 通用错误码
	CodeInternal          ErrorCode = "INTERNAL_ERROR"
	CodeInvalidArgument   ErrorCode = "INVALID_ARGUMENT"
	CodeNotFound          ErrorCode = "NOT_FOUND"
	CodeAlreadyExists     ErrorCode = "ALREADY_EXISTS"
	CodePermissionDenied  ErrorCode = "PERMISSION_DENIED"
	CodeResourceExhausted ErrorCode = "RESOURCE_EXHAUSTED"
	CodeUnavailable       ErrorCode = "UNAVAILABLE"

	// 业务错误码
	CodeInvalidVideoFile  ErrorCode = "INVALID_VIDEO_FILE"
	CodeFileTooLarge      ErrorCode = "FILE_TOO_LARGE"
	CodeFileTooSmall      ErrorCode = "FILE_TOO_SMALL"
	CodeUnsupportedFormat ErrorCode = "UNSUPPORTED_FORMAT"
	CodeInvalidAspectRatio ErrorCode = "INVALID_ASPECT_RATIO"
	CodeStorageError       ErrorCode = "STORAGE_ERROR"
	CodeTranscodeError     ErrorCode = "TRANSCODE_ERROR"
)

// AppError 应用错误
type AppError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Cause   error     `json:"-"`
}

// New 创建新的 AppError
func New(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Newf 创建带格式化的 AppError
func Newf(code ErrorCode, format string, args ...any) *AppError {
	return &AppError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// Wrap 包装错误为 AppError
func Wrap(code ErrorCode, message string, cause error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// Wrapf 包装错误为带格式化的 AppError
func Wrapf(code ErrorCode, cause error, format string, args ...any) *AppError {
	return &AppError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   cause,
	}
}

// Error 实现 error 接口
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s (cause: %v)", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 实现 errors.Unwrap 接口
func (e *AppError) Unwrap() error {
	return e.Cause
}

// Is 实现 errors.Is 接口
func (e *AppError) Is(target error) bool {
	var t *AppError
	if errors.As(target, &t) {
		return e.Code == t.Code
	}
	return false
}

// IsAppError 检查错误是否是 AppError
func IsAppError(err error) (*AppError, bool) {
	var e *AppError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// HasCode 检查错误是否包含指定的错误码
func HasCode(err error, code ErrorCode) bool {
	if e, ok := IsAppError(err); ok {
		return e.Code == code
	}
	return false
}

// 便捷函数创建常见错误

// InvalidArgument 创建无效参数错误
func InvalidArgument(message string) *AppError {
	return New(CodeInvalidArgument, message)
}

// InvalidArgumentf 创建无效参数错误（带格式化）
func InvalidArgumentf(format string, args ...any) *AppError {
	return Newf(CodeInvalidArgument, format, args...)
}

// NotFound 创建未找到错误
func NotFound(message string) *AppError {
	return New(CodeNotFound, message)
}

// NotFoundf 创建未找到错误（带格式化）
func NotFoundf(format string, args ...any) *AppError {
	return Newf(CodeNotFound, format, args...)
}

// AlreadyExists 创建已存在错误
func AlreadyExists(message string) *AppError {
	return New(CodeAlreadyExists, message)
}

// AlreadyExistsf 创建已存在错误（带格式化）
func AlreadyExistsf(format string, args ...any) *AppError {
	return Newf(CodeAlreadyExists, format, args...)
}

// Internal 创建内部错误
func Internal(message string) *AppError {
	return New(CodeInternal, message)
}

// Internalf 创建内部错误（带格式化）
func Internalf(format string, args ...any) *AppError {
	return Newf(CodeInternal, format, args...)
}

// InternalWrap 创建内部错误并包装 cause
func InternalWrap(message string, cause error) *AppError {
	return Wrap(CodeInternal, message, cause)
}

// InvalidVideoFile 创建无效视频文件错误
func InvalidVideoFile(message string) *AppError {
	return New(CodeInvalidVideoFile, message)
}

// InvalidVideoFilef 创建无效视频文件错误（带格式化）
func InvalidVideoFilef(format string, args ...any) *AppError {
	return Newf(CodeInvalidVideoFile, format, args...)
}

// FileTooLarge 创建文件过大错误
func FileTooLarge(maxSize int64) *AppError {
	return Newf(CodeFileTooLarge, "file too large (max: %d bytes)", maxSize)
}

// FileTooSmall 创建文件过小错误
func FileTooSmall(minSize int64) *AppError {
	return Newf(CodeFileTooSmall, "file too small (min: %d bytes)", minSize)
}

// UnsupportedFormat 创建不支持格式错误
func UnsupportedFormat(format string) *AppError {
	return Newf(CodeUnsupportedFormat, "unsupported format: %s", format)
}

// InvalidAspectRatio 创建无效宽高比错误
func InvalidAspectRatio(message string) *AppError {
	return New(CodeInvalidAspectRatio, message)
}

// StorageError 创建存储错误
func StorageError(message string, cause error) *AppError {
	return Wrap(CodeStorageError, message, cause)
}

// TranscodeError 创建转码错误
func TranscodeError(message string, cause error) *AppError {
	return Wrap(CodeTranscodeError, message, cause)
}
