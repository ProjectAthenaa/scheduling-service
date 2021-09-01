package scheduler

import "github.com/ProjectAthenaa/sonic-core/sonic/database/ent/product"

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
