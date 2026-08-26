package domain

import (
	"strings"
)

type ValidationIssue struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (i ValidationIssue) Error() string { return i.Field + ": " + i.Message }

type ValidationErrors struct{ Issues []ValidationIssue }

func (e *ValidationErrors) Error() string {
	if len(e.Issues) == 0 {
		return "参数校验失败"
	}
	return e.Issues[0].Error()
}

func NewValidationError(issues ...ValidationIssue) error {
	if len(issues) == 0 {
		return nil
	}
	return &ValidationErrors{Issues: issues}
}

func ValidateBatchInput(segmentID, source, actor string, p SegmentProfile) []ValidationIssue {
	issues := p.Validate()
	if strings.TrimSpace(segmentID) == "" {
		issues = append(issues, ValidationIssue{"segmentId", "管段编号不能为空"})
	}
	if strings.TrimSpace(source) == "" {
		issues = append(issues, ValidationIssue{"waterSource", "供水来源不能为空"})
	}
	if strings.TrimSpace(actor) == "" {
		issues = append(issues, ValidationIssue{"actor", "操作者不能为空"})
	}
	return issues
}
