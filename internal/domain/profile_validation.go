package domain

import "strings"

func (p SegmentProfile) Validate() []ValidationIssue {
	issues := []ValidationIssue{}
	start, end := strings.TrimSpace(p.StartMarker), strings.TrimSpace(p.EndMarker)
	if start == "" {
		issues = append(issues, ValidationIssue{"startMarker", "起点不能为空"})
	}
	if end == "" {
		issues = append(issues, ValidationIssue{"endMarker", "终点不能为空"})
	}
	if start != "" && end != "" && start == end {
		issues = append(issues, ValidationIssue{"endMarker", "起止点不能相同"})
	}
	if strings.TrimSpace(p.Material) == "" {
		issues = append(issues, ValidationIssue{"material", "材质不能为空"})
	}
	if p.VolumeM3 <= 0 {
		issues = append(issues, ValidationIssue{"volumeM3", "容积必须大于零"})
	}
	if p.LengthM < 0 {
		issues = append(issues, ValidationIssue{"lengthM", "长度不能为负"})
	}
	if p.DiameterMM < 0 {
		issues = append(issues, ValidationIssue{"diameterMm", "管径不能为负"})
	}
	if p.DiameterMM > 0 && p.LengthM == 0 {
		issues = append(issues, ValidationIssue{"lengthM", "管径和长度必须成对填写"})
	}
	if p.LengthM > 0 && p.DiameterMM == 0 {
		issues = append(issues, ValidationIssue{"diameterMm", "管径和长度必须成对填写"})
	}
	if p.TargetChlorineMin < 0 {
		issues = append(issues, ValidationIssue{"targetChlorineMin", "余氯下限不能为负"})
	}
	if p.TargetChlorineMax <= 0 {
		issues = append(issues, ValidationIssue{"targetChlorineMax", "余氯上限必须大于零"})
	}
	if p.TargetChlorineMin > p.TargetChlorineMax {
		issues = append(issues, ValidationIssue{"targetChlorineMax", "余氯上限不能小于下限"})
	}
	return issues
}
