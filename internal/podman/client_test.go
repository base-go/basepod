package podman

import (
	"strings"
	"testing"
)

func TestDecodeContainerStatsSupportsLegacyNumericFields(t *testing.T) {
	t.Parallel()

	stats, err := decodeContainerStats(strings.NewReader(`{
		"CPU": 12.5,
		"MemUsage": 1024,
		"MemLimit": 2048,
		"NetInput": 128,
		"NetOutput": 256
	}`))
	if err != nil {
		t.Fatalf("decodeContainerStats returned error: %v", err)
	}

	if stats.CPUPercent != 12.5 || stats.MemUsage != 1024 || stats.MemLimit != 2048 || stats.NetInput != 128 || stats.NetOutput != 256 {
		t.Fatalf("unexpected legacy stats decode: %+v", stats)
	}
}

func TestDecodeContainerStatsSupportsPodmanSummaryStrings(t *testing.T) {
	t.Parallel()

	stats, err := decodeContainerStats(strings.NewReader(`{
		"cpu_percent": "0.33%",
		"mem_usage": "2.093MB / 2.038GB",
		"net_io": "1.29kB / 628B"
	}`))
	if err != nil {
		t.Fatalf("decodeContainerStats returned error: %v", err)
	}

	if stats.CPUPercent != 0.33 {
		t.Fatalf("expected cpu percent 0.33, got %v", stats.CPUPercent)
	}
	if stats.MemUsage != 2093000 {
		t.Fatalf("expected mem usage 2093000, got %d", stats.MemUsage)
	}
	if stats.MemLimit != 2038000000 {
		t.Fatalf("expected mem limit 2038000000, got %d", stats.MemLimit)
	}
	if stats.NetInput != 1290 {
		t.Fatalf("expected net input 1290, got %d", stats.NetInput)
	}
	if stats.NetOutput != 628 {
		t.Fatalf("expected net output 628, got %d", stats.NetOutput)
	}
}

func TestDecodeContainerStatsSupportsDockerCompatiblePayloads(t *testing.T) {
	t.Parallel()

	stats, err := decodeContainerStats(strings.NewReader(`{
		"cpu_stats": {
			"cpu_usage": {"total_usage": 200000000},
			"system_cpu_usage": 3000000000,
			"online_cpus": 2
		},
		"precpu_stats": {
			"cpu_usage": {"total_usage": 100000000},
			"system_cpu_usage": 1000000000
		},
		"memory_stats": {
			"usage": 1048576,
			"limit": 2097152
		},
		"networks": {
			"eth0": {"rx_bytes": 1234, "tx_bytes": 5678}
		}
	}`))
	if err != nil {
		t.Fatalf("decodeContainerStats returned error: %v", err)
	}

	if stats.CPUPercent != 10 {
		t.Fatalf("expected cpu percent 10, got %v", stats.CPUPercent)
	}
	if stats.MemUsage != 1048576 || stats.MemLimit != 2097152 || stats.NetInput != 1234 || stats.NetOutput != 5678 {
		t.Fatalf("unexpected docker-compatible stats decode: %+v", stats)
	}
}
