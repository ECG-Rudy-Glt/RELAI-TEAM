package mcp

import "testing"

func TestPrioRankOrdersUrgentFirstUnknownLast(t *testing.T) {
	if !(prioRank("P0") < prioRank("P1") && prioRank("P4") < prioRank("") && prioRank("x") == prioRank("")) {
		t.Fatal("P0 < P1 ... < P4 < unknown expected")
	}
}
