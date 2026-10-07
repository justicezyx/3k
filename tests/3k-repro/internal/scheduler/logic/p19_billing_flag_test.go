package logic

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func osRead(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(moduleRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestP19_DefaultConfigSkipsBillAndBalanceStop is a source-text match of the
// checked-in yaml and of the early return in the two managers. It does not
// call Update. The call-counting check is TestP19_ManagersReturnBeforeAnyWrite
// in package pay, which this package cannot import.
func TestP19_DefaultConfigSkipsBillAndBalanceStop(t *testing.T) {
	def := osRead(t, "cmd/scheduler/etc/scheduler-api.yaml")
	prod := osRead(t, "cmd/scheduler/etc/scheduler-api_prod.yaml")
	if !strings.Contains(def, "CronBilling: No") || !strings.Contains(def, "CronBalance: No") {
		t.Fatal("default scheduler-api.yaml does not turn both cron flags off")
	}
	if !strings.Contains(prod, "CronBilling: Yes") || !strings.Contains(prod, "CronBalance: Yes") {
		t.Fatal("prod yaml does not turn both cron flags on")
	}
	billing := osRead(t, "internal/scheduler/pay/billing.go")
	balance := osRead(t, "internal/scheduler/pay/balance.go")
	if !strings.Contains(billing, "if !bm.svcCtx.Config.Billing.CronBilling {\n\t\treturn\n\t}") {
		t.Fatal("BillingManager.Update does not return immediately when CronBilling is off")
	}
	if !strings.Contains(balance, "if !bm.svcCtx.Config.Billing.CronBalance {\n\t\treturn\n\t}") {
		t.Fatal("BalanceManager.Update does not return immediately when CronBalance is off")
	}
}
