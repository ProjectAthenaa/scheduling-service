package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/protos/module"
	"github.com/ProjectAthenaa/sonic-core/sonic"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/accountgroup"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"github.com/go-redis/redis/v8"
	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis/goredis/v8"
	"github.com/json-iterator/go"
	"github.com/prometheus/common/log"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	json      = jsoniter.ConfigFastest
	redisSync *redsync.Redsync
)

func init() {
	rdb := core.Base.GetRedis("cache")
	pool := goredis.NewPool(rdb)
	redisSync = redsync.New(pool)
}

//Task is a superset of ent.Task, it holds scheduler-specific fields
type Task struct {
	*ent.Task
	monitorChannel    string
	subscriptionToken string
	controlToken      string
	userID            string
	taskID            string
	ctx               context.Context
	cancel            context.CancelFunc
	monitorStarted    bool
	//taskStarted       bool
	startTime        time.Time
	startMutex       *sync.Mutex
	dataLock         *sync.Mutex
	payload          *module.Data
	account          *account
	site             product.Site
	monitorStartTime time.Time
}

//getMonitorID returns the monitor id of a task based on its lookup values
func (t *Task) getMonitorID() string {
	prefix := fmt.Sprintf("monitors:%s:", t.site)
	v := t.Edges.Product[0]
	switch v.LookupType {
	case product.LookupTypeLink:
		return prefix + helpers.SHA1(v.Link)
	case product.LookupTypeKeywords:
		sort.Strings(v.PositiveKeywords)
		sort.Strings(v.NegativeKeywords)

		for i, s := range v.PositiveKeywords {
			v.PositiveKeywords[i] = strings.ToLower(s)
		}
		for i, s := range v.NegativeKeywords {
			v.NegativeKeywords[i] = strings.ToLower(s)
		}

		return prefix + helpers.SHA1(strings.Join(v.PositiveKeywords, "")+strings.Join(v.NegativeKeywords, ""))
	case product.LookupTypeOther:
		for k, val := range v.Metadata {
			if strings.Contains(k, "LOOKUP_") {
				return prefix + helpers.SHA1(val)
			}
		}
	}
	return ""
}

//start, calls the internal process method as a goroutine
func (t *Task) start(ctx context.Context) {
	t.process(ctx)
	return
}

func (t *Task) process(ctx context.Context) {
	mu := redisSync.NewMutex(fmt.Sprintf(fmt.Sprintf("tasks:started:%s:mutex", t.ID.String())))
	muCtx, _ := context.WithTimeout(t.ctx, time.Minute)
	err := mu.LockContext(muCtx)
	if err != nil {
		return
	}
	defer func(mu *redsync.Mutex, ctx context.Context) {
		_, err := mu.UnlockContext(ctx)
		if err != nil {

		}
	}(mu, muCtx)

	log.Info("Task Started: ", t.taskStarted())
	if t.taskStarted() || t.stopped(){
		return
	}
	log.Info("Checked if task has started")

	payload, err := t.getPayload()
	if err != nil {
		t.setStatus(module.STATUS_ERROR, "No Account")
		return
	}

	log.Info("Constructed Payload")

	started, err := Modules[t.site].Task(ctx, payload)
	if err != nil {
		log.Error("start task: ", err)
		return
	}

	if started.Started {
		t.setStarted()
		log.Info("Task Started | ", t.ID)
		go t.processUpdates()
	}

}

func (t *Task) taskStarted() bool {
	return !(rdb.Get(context.Background(), fmt.Sprintf("tasks:started:%s", t.ID.String())).Val() == "")
}

func (t *Task) setStarted() {
	rdb.SetNX(context.Background(), fmt.Sprintf("tasks:started:%s", t.ID.String()), "1", redis.KeepTTL)
}

func (t *Task) stopped() bool {
	return rdb.Get(context.Background(), fmt.Sprintf("tasks:started:%s", t.ID.String())).Val() == "2"
}

func (t *Task) processUpdates() {
	pubsub := core.Base.GetRedis("cache").Subscribe(t.ctx, fmt.Sprintf("tasks:updates:%s", t.subscriptionToken))

	var status *module.Status

	for msg := range pubsub.Channel() {
		if err := json.Unmarshal([]byte(msg.Payload), &status); err != nil {
			t.stop()
			return
		}

		switch status.Status {
		case module.STATUS_STOPPED, module.STATUS_CHECKED_OUT:
			//log.Info("Task stopped or checkout, releasing resources...", " | ", &t)
			go t.release()
		default:
			continue
		}

	}
}

//getPayload retrieves the initial payload needed to start the task
func (t *Task) getPayload() (*module.Data, error) {
	t.dataLock.Lock()
	defer t.dataLock.Unlock()
	//defer func() {
	//	if err := recover(); err != nil {
	//		log.Error("error creating payload: ", err)
	//		t.payload = nil
	//	}
	//}()
	var mData *module.Data

	if f := t.payload; f != nil {
		return t.payload, nil
	}

	prod := t.Edges.Product[0]

	proxy, err := t.getProxy()
	if err != nil {
		return t.payload, nil
	}
	mData = &module.Data{
		Proxy: &module.Proxy{
			Username: proxy.Username,
			Password: proxy.Password,
			IP:       proxy.IP,
			Port:     proxy.Port,
		},
		TaskData: &module.TaskData{
			Color: prod.Colors,
			Size:  prod.Sizes,
		},
		Channels: &module.Channels{
			MonitorChannel:  t.getMonitorID(),
			UpdatesChannel:  t.subscriptionToken,
			CommandsChannel: t.controlToken,
		},
	}

	if len(prod.Colors) == 0 || prod.Colors[0] == "random" {
		mData.TaskData.Color = []string{"0"}
		mData.TaskData.RandomColor = true
	}

	if len(prod.Sizes) == 0 || prod.Sizes[0] == "random" {
		mData.TaskData.Size = []string{"0"}
		mData.TaskData.RandomSize = true
	}

	mData.Profile, err = t.getProfile()
	if err != nil {
		return t.payload, err
	}

	mData.Metadata = prod.Metadata
	if siteNeedsAccount[t.site] {
		mData.Metadata["username"], mData.Metadata["password"], err = t.getAccount()
		if err != nil {
			return t.payload, err
		}
	}

	mData.TaskID = t.taskID

	t.payload = mData
	return t.payload, nil
}

func (t *Task) stop() {
	core.Base.GetRedis("cache").Publish(t.ctx, fmt.Sprintf("tasks:commands:%s", t.controlToken), "STOP")
}

func (t *Task) setStatus(status module.STATUS, msg string) {
	stat := &module.Status{Information: make(map[string]string)}
	stat.Information["msg"] = msg
	stat.Status = status
	data, err := json.Marshal(stat)
	if err != nil {
		log.Error("failed to marshal status: ", err)
		return
	}

	core.Base.GetRedis("cache").Publish(t.ctx, "tasks:updates:%s", string(data))
}

func (t *Task) release() {
	core.Base.GetRedis("cache").SRem(context.Background(), "scheduler:processing", t.taskID)
	rdb.Set(context.Background(), fmt.Sprintf("tasks:started:%s", t.ID.String()), "2", redis.KeepTTL)
}

func (t *Task) getProxy() (*module.Proxy, error) {
	rdb := core.Base.GetRedis("cache")
	dbProxyList := t.Edges.ProxyList[0]

	dbProxies, err := dbProxyList.Proxies(t.ctx)
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("tasks:proxies:%s", dbProxyList.ID.String())

	locker := redisSync.NewMutex(key + ":locker")

	if err = locker.LockContext(t.ctx); err != nil {
		log.Error("error acquiring proxy mutex: ", err)
		return nil, err
	}

	defer func() {
		if ok, err := locker.UnlockContext(t.ctx); !ok || err != nil {
			log.Error("error unlocking proxy mutex: ", err)
		}
	}()

	proxies := rdb.SMembers(t.ctx, key).Val()

	if len(proxies) == 0 {
		var availablePool []interface{}

		for _, proxy := range dbProxies {
			var payload []byte
			if payload, err = json.Marshal(&proxy); err != nil {
				continue
			}
			availablePool = append(availablePool, string(payload))
		}

		rdb.SAdd(t.ctx, key, availablePool[1:]...)

		return &module.Proxy{
			Username: &dbProxies[0].Username,
			Password: &dbProxies[0].Password,
			IP:       dbProxies[0].IP,
			Port:     dbProxies[0].Port,
		}, nil
	}

	var proxy *module.Proxy

	data := core.Base.GetRedis("cache").SPop(t.ctx, key).Val()

	if err = json.Unmarshal([]byte(data), &proxy); err != nil {
		return nil, err
	}

	return proxy, nil
}

func (t *Task) getAccount() (username, password string, err error) {
	rdb := core.Base.GetRedis("cache")

	app, _ := t.Edges.TaskGroup.App(t.ctx)
	// J9K6W:VGAX7JUK:194.163.219.108:7924
	dbAccounts, err := app[0].QueryAccountGroups().Where(accountgroup.SiteEQ(accountgroup.Site(t.site))).First(t.ctx)
	if err != nil {
		return "", "", sonic.EntErr(err)
	}

	key := fmt.Sprintf("tasks:accounts:%s", dbAccounts.ID.String())

	locker := redisSync.NewMutex(key + ":locker")

	if err = locker.LockContext(t.ctx); err != nil {
		log.Error("error acquiring account group mutex: ", err)
		return "", "", err
	}

	defer func() {
		if ok, err := locker.UnlockContext(t.ctx); !ok || err != nil {
			log.Error("error unlocking account group mutex: ", err)
		}
	}()

	accounts := rdb.SMembers(t.ctx, key).Val()

	if len(accounts) == 0 {
		var availablePool []interface{}

		for u, p := range dbAccounts.Accounts {
			availablePool = append(availablePool, fmt.Sprintf("%s:%s", u, p))
		}

		rdb.SAdd(t.ctx, key, availablePool[1:]...)

		acc := strings.Split(availablePool[0].(string), ":")

		return acc[0], acc[1], nil
	}

	data := core.Base.GetRedis("cache").SPop(t.ctx, key).Val()

	acc := strings.Split(data, ":")

	return acc[0], acc[1], nil
}

func (t *Task) getProfile() (retProf *module.Profile, err error) {
	rdb := core.Base.GetRedis("cache")

	profileGroup := t.Edges.ProfileGroup

	key := fmt.Sprintf("tasks:profiles:%s", profileGroup.ID.String())

	locker := redisSync.NewMutex(key + ":locker")

	if err := locker.LockContext(t.ctx); err != nil {
		log.Error("error acquiring profile group mutex: ", err)
		return nil, err
	}

	defer func() {
		if ok, err := locker.UnlockContext(t.ctx); !ok || err != nil {
			log.Error("error unlocking account group mutex: ", err)
		}
	}()

	accounts := rdb.SMembers(t.ctx, key).Val()

	if len(accounts) == 0 {
		var availablePool []interface{}
		var toAppend *module.Profile

		profiles, err := profileGroup.Profiles(t.ctx)
		if err != nil {
			return nil, sonic.EntErr(err)
		}

		for i, prof := range profiles {
			shipping, err := prof.QueryShipping().First(t.ctx)
			if err != nil {
				return nil, sonic.EntErr(err)
			}

			shippingAddress, err := shipping.QueryShippingAddress().First(t.ctx)
			if err != nil {
				return nil, sonic.EntErr(err)
			}

			billing, err := prof.QueryBilling().First(t.ctx)
			if err != nil {
				return nil, sonic.EntErr(err)
			}

			toAppend = &module.Profile{
				Email: prof.Email,
				Shipping: &module.Shipping{
					FirstName:   shipping.FirstName,
					LastName:    shipping.LastName,
					PhoneNumber: shipping.PhoneNumber,
					ShippingAddress: &module.Address{
						AddressLine:  shippingAddress.AddressLine,
						AddressLine2: &shippingAddress.AddressLine2,
						Country:      shippingAddress.Country,
						State:        shippingAddress.State,
						City:         shippingAddress.City,
						ZIP:          shippingAddress.ZIP,
					},
					BillingAddress:    nil,
					BillingIsShipping: shipping.BillingIsShipping,
				},
				Billing: &module.Billing{
					Number:          billing.CardNumber,
					ExpirationMonth: billing.ExpiryMonth,
					ExpirationYear:  billing.ExpiryYear,
					CVV:             billing.CVV,
				},
			}

			if !shipping.BillingIsShipping {
				billingAddress, err := shipping.QueryBillingAddress().First(t.ctx)
				if err != nil {
					log.Error("query billing address: ", err)
					panic(err)
				}
				toAppend.Shipping.BillingAddress = &module.Address{
					AddressLine:  billingAddress.AddressLine,
					AddressLine2: &billingAddress.AddressLine2,
					Country:      billingAddress.Country,
					State:        billingAddress.State,
					City:         billingAddress.City,
					ZIP:          billingAddress.ZIP,
				}
			}

			payload, err := json.Marshal(&toAppend)
			if err != nil {
				return nil, err
			}

			if i == 0 {
				retProf = toAppend
			}

			availablePool = append(availablePool, string(payload))
		}

		rdb.SAdd(t.ctx, key, availablePool[1:]...)

		return retProf, nil
	}

	data := core.Base.GetRedis("cache").SPop(t.ctx, key).Val()

	var prof *module.Profile

	if err := json.Unmarshal([]byte(data), &prof); err != nil {
		return nil, err
	}

	return prof, nil
}
