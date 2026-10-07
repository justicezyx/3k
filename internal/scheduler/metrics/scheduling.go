// Package metrics holds Prometheus instrumentation for the Portal scheduler service.
package metrics

// TODO(YXZ-15): Register scheduler_placement_total and scheduler_placement_score.
// https://linear.app/yxzhao/issue/YXZ-15/scheduler-prometheus-placement-metrics-and-unified-wns-observability
// Wire Inc/Observe from recordSchedulingObs in cpod_schedule.go (CpodJob always uses WNS).
