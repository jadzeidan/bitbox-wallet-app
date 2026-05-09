// SPDX-License-Identifier: Apache-2.0

package backend

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateMoneyWeightedReturnNoCashFlows(t *testing.T) {
	startTime := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	endTime := startTime.Add(24 * time.Hour)

	result := calculateMoneyWeightedReturn(100, 110, startTime, endTime, nil)

	require.NotNil(t, result)
	require.InDelta(t, 0.1, *result, 1e-12)
}

func TestChartPerformanceForRangeUsesFirstPositiveEntry(t *testing.T) {
	now := time.Date(2026, time.January, 4, 12, 0, 0, 0, time.UTC)
	entries := []ChartEntry{
		{Time: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix(), Value: 0},
		{Time: time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix(), Value: 0},
		{Time: time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC).Unix(), Value: 100},
	}

	performance := chartPerformanceForRange(entries, nil, time.Time{}, now, 110)

	require.NotNil(t, performance.MoneyWeightedReturn)
	require.InDelta(t, 0.1, *performance.MoneyWeightedReturn, 1e-12)
}

func TestChartPerformanceForRangeReturnsNilWhenCashFlowValueUnavailable(t *testing.T) {
	startTime := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	endTime := startTime.Add(24 * time.Hour)
	entries := []ChartEntry{
		{Time: startTime.Unix(), Value: 100},
	}
	cashFlows := []chartCashFlow{
		{
			Time:           startTime.Add(12 * time.Hour),
			ValueAvailable: false,
		},
	}

	performance := chartPerformanceForRange(entries, cashFlows, time.Time{}, endTime, 110)

	require.Nil(t, performance.MoneyWeightedReturn)
}
