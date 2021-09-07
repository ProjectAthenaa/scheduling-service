package scheduler

import (
	"context"
	"errors"
	"fmt"
	"github.com/ProjectAthenaa/sonic-core/protos/module"
	"github.com/ProjectAthenaa/sonic-core/sonic/core"
	"github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"
	"google.golang.org/grpc"
	"strings"
)

var siteMonitors = map[product.Site]bool{
	product.SiteFinishLine:     true,
	product.SiteJD_Sports:      true,
	product.SiteYeezySupply:    true,
	product.SiteSupreme:        true,
	product.SiteEastbay_US:     true,
	product.SiteChamps_US:      true,
	product.SiteFootaction_US:  true,
	product.SiteFootlocker_US:  true,
	product.SiteBestbuy:        true,
	product.SitePokemon_Center: true,
	product.SitePanini_US:      true,
	product.SiteTopss:          true,
	product.SiteNordstorm:      true,
	product.SiteEnd:            true,
	product.SiteTarget:         false,
	product.SiteAmazon:         true,
	product.SiteSolebox:        true,
	product.SiteOnygo:          true,
	product.SiteSnipes:         true,
	product.SiteSsense:         true,
	product.SiteWalmart:        true,
	product.SiteHibbet:         true,
	product.SiteNewBalance:     true,
}

var siteNeedsAccount = map[product.Site]bool{
	product.SiteFinishLine:     false,
	product.SiteJD_Sports:      false,
	product.SiteYeezySupply:    false,
	product.SiteSupreme:        false,
	product.SiteEastbay_US:     false,
	product.SiteChamps_US:      false,
	product.SiteFootaction_US:  false,
	product.SiteFootlocker_US:  false,
	product.SiteBestbuy:        false,
	product.SitePokemon_Center: false,
	product.SitePanini_US:      false,
	product.SiteTopss:          false,
	product.SiteNordstorm:      false,
	product.SiteEnd:            false,
	product.SiteTarget:         true,
	product.SiteAmazon:         false,
	product.SiteSolebox:        false,
	product.SiteOnygo:          false,
	product.SiteSnipes:         false,
	product.SiteSsense:         false,
	product.SiteWalmart:        false,
	product.SiteHibbet:         false,
	product.SiteNewBalance:     false,
}

type account struct {
	username string
	password string
}

var siteAccounts = map[product.Site]func(tk *Task) (*account, error){
	product.SiteTarget: func(tk *Task) (*account, error) {
		fmt.Println(core.Base.GetRedis("cache").Ping(context.Background()))
		setKey := fmt.Sprintf("accounts:target:%s", tk.userID)
		acc, err := core.Base.GetRedis("cache").SPop(context.Background(), setKey).Result()

		if err != nil {
			return nil, err
		}

		if len(acc) == 0 {
			return nil, errors.New("no_account")
		}

		details := strings.Split(acc, ":")

		return &account{
			username: details[0],
			password: details[1],
		}, nil
	},
}

//Modules is the map that holds all the clients for the different modules in siteMap
var Modules = map[product.Site]module.ModuleClient{}

//moduleMap holds the DNS names for all the modules
var siteMap = map[product.Site]string{
	product.SiteFinishLine:     "finishline.modules.svc.cluster.local:3000",
	product.SiteJD_Sports:      "jdsports.modules.svc.cluster.local:3000",
	product.SiteYeezySupply:    "yeezysupply.modules.svc.cluster.local:3000",
	product.SiteSupreme:        "supreme.modules.svc.cluster.local:3000",
	product.SiteEastbay_US:     "eastbayUS.modules.svc.cluster.local:3000",
	product.SiteChamps_US:      "champsUS.modules.svc.cluster.local:3000",
	product.SiteFootaction_US:  "footactionUS.modules.svc.cluster.local:3000",
	product.SiteFootlocker_US:  "footlockerUS.modules.svc.cluster.local:3000",
	product.SiteBestbuy:        "bestbuy.modules.svc.cluster.local:3000",
	product.SitePokemon_Center: "pokemon-center.modules.svc.cluster.local:3000",
	product.SitePanini_US:      "paniniUS.modules.svc.cluster.local:3000",
	product.SiteTopss:          "topps.modules.svc.cluster.local:3000",
	product.SiteNordstorm:      "nordstorm.modules.svc.cluster.local:3000",
	product.SiteEnd:            "end.modules.svc.cluster.local:3000",
	product.SiteTarget:         "target.modules.svc.cluster.local:3000",
	product.SiteAmazon:         "amazon.modules.svc.cluster.local:3000",
	product.SiteSolebox:        "solebox.modules.svc.cluster.local:3000",
	product.SiteOnygo:          "onygo.modules.svc.cluster.local:3000",
	product.SiteSnipes:         "snipes.modules.svc.cluster.local:3000",
	product.SiteSsense:         "ssense.modules.svc.cluster.local:3000",
	product.SiteWalmart:        "walmart.modules.svc.cluster.local:3000",
	product.SiteHibbet:         "hibbet.modules.svc.cluster.local:3000",
}

//populateMap connects to all the services shown in siteMap and populates the Modules map accordingly
func populateMap() error {
	for k, v := range siteMap {
		conn, err := grpc.Dial(v, grpc.WithInsecure())
		if err != nil {
			panic(err)
		}

		Modules[k] = module.NewModuleClient(conn)
	}
	return nil
}
