package pay

import (
	"context"
	"database/sql"
	"testing"

	"sxwl/3k/internal/scheduler/config"
	"sxwl/3k/internal/scheduler/model"
	"sxwl/3k/internal/scheduler/svc"

	"github.com/Masterminds/squirrel"
	"github.com/zeromicro/go-zero/core/stores/sqlx"
)

type calls struct{ n int }

func (c *calls) hit() { c.n++ }

// Recording stubs stand in for the model interfaces Update touches after the
// cron-flag check. With the flags off, every counter stays zero. With a flag
// on, the first read increments the counter, so an ignored flag cannot pass.

type priceStub struct{ c *calls }

func (s priceStub) Insert(context.Context, *model.SysPrice) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) FindOne(context.Context, int64) (*model.SysPrice, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) Update(context.Context, *model.SysPrice) error { s.c.hit(); return nil }
func (s priceStub) Delete(context.Context, int64) error           { s.c.hit(); return nil }
func (s priceStub) AllFieldsBuilder() squirrel.SelectBuilder {
	s.c.hit()
	return squirrel.Select("*")
}
func (s priceStub) UpdateBuilder() squirrel.UpdateBuilder {
	s.c.hit()
	return squirrel.Update("sys_price")
}
func (s priceStub) FindOneByQuery(context.Context, squirrel.SelectBuilder) (*model.SysPrice, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) FindOneById(context.Context, *model.SysPrice) (*model.SysPrice, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) FindAll(context.Context, string) ([]*model.SysPrice, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) Find(context.Context, squirrel.SelectBuilder) ([]*model.SysPrice, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) FindPageListByPage(context.Context, squirrel.SelectBuilder, int64, int64, string) ([]*model.SysPrice, error) {
	s.c.hit()
	return nil, nil
}
func (s priceStub) UpdateColsByCond(context.Context, squirrel.UpdateBuilder) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}

type jobStub struct{ c *calls }

func (s jobStub) Insert(context.Context, *model.SysUserJob) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}
func (s jobStub) FindOne(context.Context, int64) (*model.SysUserJob, error) {
	s.c.hit()
	return nil, nil
}
func (s jobStub) Update(context.Context, *model.SysUserJob) error { s.c.hit(); return nil }
func (s jobStub) Delete(context.Context, int64) error             { s.c.hit(); return nil }
func (s jobStub) AllFieldsBuilder() squirrel.SelectBuilder {
	s.c.hit()
	return squirrel.Select("*")
}
func (s jobStub) UpdateBuilder() squirrel.UpdateBuilder {
	s.c.hit()
	return squirrel.Update("sys_user_job")
}
func (s jobStub) DeleteSoft(context.Context, *model.SysUserJob) error { s.c.hit(); return nil }
func (s jobStub) DeleteSoftByName(context.Context, string) error      { s.c.hit(); return nil }
func (s jobStub) FindOneByQuery(context.Context, squirrel.SelectBuilder) (*model.SysUserJob, error) {
	s.c.hit()
	return nil, nil
}
func (s jobStub) FindOneById(context.Context, *model.SysUserJob) (*model.SysUserJob, error) {
	s.c.hit()
	return nil, nil
}
func (s jobStub) FindAll(context.Context, string) ([]*model.SysUserJob, error) {
	s.c.hit()
	return nil, nil
}
func (s jobStub) Find(context.Context, squirrel.SelectBuilder) ([]*model.SysUserJob, error) {
	s.c.hit()
	return nil, nil
}
func (s jobStub) FindPageListByPage(context.Context, any, int64, int64, string) ([]*model.SysUserJob, int64, error) {
	s.c.hit()
	return nil, 0, nil
}
func (s jobStub) UpdateColsByCond(context.Context, squirrel.UpdateBuilder) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}

type inferStub struct{ c *calls }

func (s inferStub) Insert(context.Context, *model.SysInference) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}
func (s inferStub) FindOne(context.Context, int64) (*model.SysInference, error) {
	s.c.hit()
	return nil, nil
}
func (s inferStub) Update(context.Context, *model.SysInference) error { s.c.hit(); return nil }
func (s inferStub) Delete(context.Context, int64) error               { s.c.hit(); return nil }
func (s inferStub) AllFieldsBuilder() squirrel.SelectBuilder {
	s.c.hit()
	return squirrel.Select("*")
}
func (s inferStub) UpdateBuilder() squirrel.UpdateBuilder {
	s.c.hit()
	return squirrel.Update("sys_inference")
}
func (s inferStub) FindOneByQuery(context.Context, squirrel.SelectBuilder) (*model.SysInference, error) {
	s.c.hit()
	return nil, nil
}
func (s inferStub) FindOneById(context.Context, *model.SysInference) (*model.SysInference, error) {
	s.c.hit()
	return nil, nil
}
func (s inferStub) FindAll(context.Context, squirrel.SelectBuilder, string) ([]*model.SysInference, error) {
	s.c.hit()
	return nil, nil
}
func (s inferStub) FindPageListByPage(context.Context, squirrel.SelectBuilder, int64, int64, string) ([]*model.SysInference, error) {
	s.c.hit()
	return nil, nil
}
func (s inferStub) UpdateColsByCond(context.Context, squirrel.UpdateBuilder) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}

type jupyterStub struct{ c *calls }

func (s jupyterStub) Insert(context.Context, *model.SysJupyterlab) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) FindOne(context.Context, int64) (*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) FindOneByJobName(context.Context, string) (*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) Update(context.Context, *model.SysJupyterlab) error { s.c.hit(); return nil }
func (s jupyterStub) Delete(context.Context, int64) error                { s.c.hit(); return nil }
func (s jupyterStub) AllFieldsBuilder() squirrel.SelectBuilder {
	s.c.hit()
	return squirrel.Select("*")
}
func (s jupyterStub) UpdateBuilder() squirrel.UpdateBuilder {
	s.c.hit()
	return squirrel.Update("sys_jupyterlab")
}
func (s jupyterStub) FindOneByQuery(context.Context, squirrel.SelectBuilder) (*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) FindOneById(context.Context, *model.SysJupyterlab) (*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) FindAll(context.Context, string) ([]*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) Find(context.Context, squirrel.SelectBuilder) ([]*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) FindPageListByPage(context.Context, squirrel.SelectBuilder, int64, int64, string) ([]*model.SysJupyterlab, error) {
	s.c.hit()
	return nil, nil
}
func (s jupyterStub) UpdateColsByCond(context.Context, squirrel.UpdateBuilder) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}

type billingStub struct{ c *calls }

func (s billingStub) Insert(context.Context, *model.UserBilling) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) FindOne(context.Context, int64) (*model.UserBilling, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) Update(context.Context, *model.UserBilling) error { s.c.hit(); return nil }
func (s billingStub) Delete(context.Context, int64) error              { s.c.hit(); return nil }
func (s billingStub) AllFieldsBuilder() squirrel.SelectBuilder {
	s.c.hit()
	return squirrel.Select("*")
}
func (s billingStub) UpdateBuilder() squirrel.UpdateBuilder {
	s.c.hit()
	return squirrel.Update("user_billing")
}
func (s billingStub) InsertBuilder() squirrel.InsertBuilder {
	s.c.hit()
	return squirrel.Insert("user_billing")
}
func (s billingStub) FindOneByQuery(context.Context, squirrel.SelectBuilder) (*model.UserBilling, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) FindOneById(context.Context, *model.UserBilling) (*model.UserBilling, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) FindAll(context.Context, string) ([]*model.UserBilling, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) Find(context.Context, squirrel.SelectBuilder) ([]*model.UserBilling, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) FindPageListByPage(context.Context, squirrel.SelectBuilder, int64, int64, string) ([]*model.UserBilling, error) {
	s.c.hit()
	return nil, nil
}
func (s billingStub) UpdateColsByCond(context.Context, squirrel.UpdateBuilder) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}

type balanceStub struct{ c *calls }

func (s balanceStub) Insert(context.Context, *model.UserBalance) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) FindOne(context.Context, int64) (*model.UserBalance, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) Update(context.Context, *model.UserBalance) error { s.c.hit(); return nil }
func (s balanceStub) Delete(context.Context, int64) error              { s.c.hit(); return nil }
func (s balanceStub) AllFieldsBuilder() squirrel.SelectBuilder {
	s.c.hit()
	return squirrel.Select("*")
}
func (s balanceStub) UpdateBuilder() squirrel.UpdateBuilder {
	s.c.hit()
	return squirrel.Update("user_balance")
}
func (s balanceStub) FindOneByQuery(context.Context, squirrel.SelectBuilder) (*model.UserBalance, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) FindOneById(context.Context, *model.UserBalance) (*model.UserBalance, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) FindAll(context.Context, string) ([]*model.UserBalance, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) Find(context.Context, squirrel.SelectBuilder) ([]*model.UserBalance, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) GetBalance(context.Context, sqlx.Session, string) (float64, error) {
	s.c.hit()
	return 0, nil
}
func (s balanceStub) SetBalance(context.Context, sqlx.Session, string, float64) error {
	s.c.hit()
	return nil
}
func (s balanceStub) FindPageListByPage(context.Context, squirrel.SelectBuilder, int64, int64, string) ([]*model.UserBalance, error) {
	s.c.hit()
	return nil, nil
}
func (s balanceStub) UpdateColsByCond(context.Context, squirrel.UpdateBuilder) (sql.Result, error) {
	s.c.hit()
	return nil, nil
}

func recordingSvc(bill, bal *calls, billingOn, balanceOn bool) *svc.ServiceContext {
	s := &svc.ServiceContext{Config: config.Config{}}
	s.Config.Billing.CronBilling = billingOn
	s.Config.Billing.CronBalance = balanceOn
	s.PriceModel = priceStub{bill}
	s.UserJobModel = jobStub{bill}
	s.InferenceModel = inferStub{bill}
	s.JupyterlabModel = jupyterStub{bill}
	s.UserBillingModel = billingStub{bill}
	s.UserBalanceModel = balanceStub{bal}
	return s
}

// TestP19_ManagersReturnBeforeAnyWrite calls Update with both cron flags off.
// The recording models stay at zero calls, so no bill row and no balance
// update ran. Turning one flag on reaches that model, which is how a skipped
// flag check would be visible.
func TestP19_ManagersReturnBeforeAnyWrite(t *testing.T) {
	bill, bal := &calls{}, &calls{}
	svcCtx := recordingSvc(bill, bal, false, false)
	NewBillingManager(svcCtx).Update()
	NewBalanceManager(svcCtx).Update()
	if bill.n != 0 || bal.n != 0 {
		t.Fatalf("flags off still touched models: billing calls=%d balance calls=%d", bill.n, bal.n)
	}

	billOn, balOn := &calls{}, &calls{}
	on := recordingSvc(billOn, balOn, true, false)
	NewBillingManager(on).Update()
	if billOn.n == 0 {
		t.Fatal("CronBilling on did not read a model; the stub is not wired")
	}
	if balOn.n != 0 {
		t.Fatalf("balance model was touched with CronBalance off: %d", balOn.n)
	}

	// CronBalance reads unpaid user_billing rows and then user_balance.
	// CronBilling being off does not skip that read; the two flags are separate.
	billOff, balOnly := &calls{}, &calls{}
	balSvc := recordingSvc(billOff, balOnly, false, true)
	NewBalanceManager(balSvc).Update()
	if billOff.n == 0 || balOnly.n == 0 {
		t.Fatalf("CronBalance on did not read billing and balance models: billing calls=%d balance calls=%d", billOff.n, balOnly.n)
	}
}
