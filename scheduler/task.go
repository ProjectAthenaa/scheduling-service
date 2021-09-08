package scheduler

import (
	"context"
	"fmt"
	"github.com/ProjectAthenaa/scheduling-service/helpers"
	"github.com/ProjectAthenaa/sonic-core/protos/module"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"github.com/go-redis/redis/v8"
	"github.com/json-iterator/go"
	"github.com/prometheus/common/log"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	json = jsoniter.ConfigFastest
)

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
	taskStarted       bool
	startTime         time.Time
	startMutex        *sync.Mutex
	dataLock          *sync.Mutex
	payload           *module.Data
	account           *account
	site              product.Site
}

//chunk takes in a slice of tasks and a chunkSize and returns a new slice of slices that has the length of chunkSize and contains
//an evenly distributed amount of Task(s)
func chunk(tasks []*Task, chunkSize int) [][]*Task {
	if len(tasks) == 0 {
		return nil
	}
	divided := make([][]*Task, (len(tasks)+chunkSize-1)/chunkSize)
	prev := 0
	i := 0
	till := len(tasks) - chunkSize
	for prev < till {
		next := prev + chunkSize
		divided[i] = tasks[prev:next]
		prev = next
		i++
	}
	divided[i] = tasks[prev:]
	return divided
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
func (t *Task) start(ctx context.Context) error {
	go t.process(ctx)
	return nil
}

func (t *Task) process(ctx context.Context) {
	log.Info(&t)
	t.startMutex.Lock()
	defer t.startMutex.Unlock()

	if t.taskStarted || time.Since(t.startTime) >= time.Second*5 {
		return
	}

	payload, err := t.getPayload()
	if err != nil {
		t.taskStarted = false
		t.setStatus(module.STATUS_ERROR, "No Account")
		return
	}

	started, err := Modules[t.site].Task(ctx, payload)
	if err != nil {
		log.Error("start task: ", err)
		return
	}

	if started.Started {
		log.Info("Task Started | ", t.ID)
		go t.processUpdates()
		t.taskStarted = true
	}

}

func (t *Task) processUpdates() {
	pubsub := core.Base.GetRedis("cache").Subscribe(t.ctx, fmt.Sprintf("tasks:updates:%s", t.subscriptionToken))

	var status *module.Status

	for msg := range pubsub.Channel() {
		if err := json.Unmarshal([]byte(msg.Payload), &status); err != nil {
			t.stop()
			go t.releaseAccount()
			return
		}

		switch status.Status {
		case module.STATUS_STOPPED, module.STATUS_CHECKED_OUT:
			go t.releaseAccount()
		default:
			continue
		}

	}
}

func (t *Task) releaseAccount() {
	blockKey := helpers.SHA1(fmt.Sprintf("%s:%s", t.account.username, t.account.password))

	if core.Base.GetRedis("cache").Exists(context.Background(), blockKey).Val() == 1 {
		core.Base.GetRedis("cache").Del(context.Background(), blockKey)
		return
	}

	accountPoolKey := fmt.Sprintf("accounts:%s:%s", t.site, t.userID)

	core.Base.GetRedis("cache").Set(context.Background(), accountPoolKey, fmt.Sprintf("%s:%s", t.account.username, t.account.password), redis.KeepTTL)
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

	var ctx = context.Background()
	rand.Seed(time.Now().UnixNano())

	pg := t.Edges.ProfileGroup

	profiles, err := pg.QueryProfiles().All(ctx)
	if err != nil {
		log.Error("query all profiles: ", err)
		panic(err)
	}

	profile := profiles[rand.Intn(len(profiles))]

	pl := t.Edges.ProxyList[0]

	proxies, err := pl.QueryProxies().All(ctx)
	if err != nil {
		log.Error("query proxies: ", err)
		panic(err)
	}

	prod := t.Edges.Product[0]

	proxy := proxies[rand.Intn(len(proxies))]
	mData = &module.Data{
		Proxy: &module.Proxy{
			Username: &proxy.Username,
			Password: &proxy.Password,
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

	shipping, err := profile.QueryShipping().First(ctx)
	if err != nil {
		log.Error("query shipping: ", err)
		panic(err)
	}
	shippingAddress, err := shipping.QueryShippingAddress().First(ctx)
	if err != nil {
		log.Error("query shipping address: ", err)
		panic(err)
	}
	billing, err := profile.QueryBilling().First(ctx)
	if err != nil {
		log.Error("query billing: ", err)
		panic(err)
	}
	mData.Profile = &module.Profile{
		Email: profile.Email,
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
		billingAddress, err := shipping.QueryBillingAddress().First(ctx)
		if err != nil {
			log.Error("query billing address: ", err)
			panic(err)
		}
		mData.Profile.Shipping.BillingAddress = &module.Address{
			AddressLine:  billingAddress.AddressLine,
			AddressLine2: &billingAddress.AddressLine2,
			Country:      billingAddress.Country,
			State:        billingAddress.State,
			City:         billingAddress.City,
			ZIP:          billingAddress.ZIP,
		}

	}

	mData.Metadata = prod.Metadata
	if siteNeedsAccount[t.site] {
		acc, err := siteAccounts[t.site](t)
		fmt.Println(acc)
		if err != nil {
			return nil, err
		}

		mData.Metadata["username"] = acc.username
		mData.Metadata["password"] = acc.password
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
