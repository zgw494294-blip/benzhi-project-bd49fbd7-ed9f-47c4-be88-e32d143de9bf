package domain

import (
	"math"
	"strings"
)

// geometryVolumeMaxDiameterMm and geometryVolumeMaxLengthM bound the diameter
// and length inputs so the cylindrical volume derivation in rules.CheckGeometry
// cannot overflow float64. They are far above any physically plausible pipe
// segment yet keep the geometric computation strictly finite and serializable.
const (
	geometryVolumeMaxDiameterMm = 1e9 // 1,000 km in millimetres
	geometryVolumeMaxLengthM   = 1e9 // 1,000,000 km in metres
)

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
	if math.IsNaN(p.VolumeM3) || math.IsInf(p.VolumeM3, 0) {
		issues = append(issues, ValidationIssue{"volumeM3", "容积必须为有限数值"})
	}
	if p.LengthM < 0 {
		issues = append(issues, ValidationIssue{"lengthM", "长度不能为负"})
	}
	if math.IsNaN(p.LengthM) || math.IsInf(p.LengthM, 0) {
		issues = append(issues, ValidationIssue{"lengthM", "长度必须为有限数值"})
	}
	if p.DiameterMM < 0 {
		issues = append(issues, ValidationIssue{"diameterMm", "管径不能为负"})
	}
	if math.IsNaN(p.DiameterMM) || math.IsInf(p.DiameterMM, 0) {
		issues = append(issues, ValidationIssue{"diameterMm", "管径必须为有限数值"})
	}
	if p.DiameterMM > 0 && p.LengthM == 0 {
		issues = append(issues, ValidationIssue{"lengthM", "管径和长度必须成对填写"})
	}
	if p.LengthM > 0 && p.DiameterMM == 0 {
		issues = append(issues, ValidationIssue{"diameterMm", "管径和长度必须成对填写"})
	}
	if p.DiameterMM > geometryVolumeMaxDiameterMm {
		issues = append(issues, ValidationIssue{"diameterMm", "管径超出可计算范围"})
	}
	if p.LengthM > geometryVolumeMaxLengthM {
		issues = append(issues, ValidationIssue{"lengthM", "长度超出可计算范围"})
	}
	if p.TargetChlorineMin < 0 {
		issues = append(issues, ValidationIssue{"targetChlorineMin", "余氯下限不能为负"})
	}
	if math.IsNaN(p.TargetChlorineMin) || math.IsInf(p.TargetChlorineMin, 0) {
		issues = append(issues, ValidationIssue{"targetChlorineMin", "余氯下限必须为有限数值"})
	}
	if p.TargetChlorineMax <= 0 {
		issues = append(issues, ValidationIssue{"targetChlorineMax", "余氯上限必须大于零"})
	}
	if math.IsNaN(p.TargetChlorineMax) || math.IsInf(p.TargetChlorineMax, 0) {
		issues = append(issues, ValidationIssue{"targetChlorineMax", "余氯上限必须为有限数值"})
	}
	if p.TargetChlorineMin > p.TargetChlorineMax {
		issues = append(issues, ValidationIssue{"targetChlorineMax", "余氯上限不能小于下限"})
	}
	return issues
}
