package cnbootstrap

import "kairisei.local/server/internal/cpk"

// clientResources is a validated, read-only inventory shared by every account.
type clientResources struct {
	Banners             map[string]string
	GachaBannerVersions map[string]string
	CPK                 []cpk.File
	Images              cardImages
	CPKList             []byte
	CPKState            []byte
	AvailableBundles    map[string]struct{}
	Platforms           platformDeliveries
}

func loadClientResources(config ResourcesConfig) (*clientResources, error) {
	platforms, err := loadPlatformDeliveries(config.PlatformDeliveries)
	if err != nil {
		return nil, err
	}
	images, err := loadCardImages(config.ImageRoot)
	if err != nil {
		return nil, err
	}
	bannerPaths, err := resolveCN602BannerPaths(config.Banners)
	if err != nil {
		return nil, err
	}
	bannerVersions, err := gachaBannerVersions(bannerPaths[""], bannerPaths)
	if err != nil {
		return nil, err
	}
	cpkAliases, err := cpk.LoadAliases(config.CPKRoot, config.CPKAliases)
	if err != nil {
		return nil, err
	}
	cpkDelivery, err := cpk.Load(config.CPKRoot, cpkAliases)
	if err != nil {
		return nil, err
	}
	cpkFileList := cpk.FileList(cpkDelivery)
	cpkPatchState := cpk.PatchState(cpkDelivery)
	return &clientResources{
		Banners:             bannerPaths,
		GachaBannerVersions: bannerVersions,
		CPK:                 cpkDelivery,
		Images:              images,
		CPKList:             cpkFileList,
		CPKState:            cpkPatchState,
		AvailableBundles:    platforms.availableBundles(),
		Platforms:           platforms,
	}, nil
}

// Admin receives only pool artwork; Home and the generic fallback are not pool choices.
func (resources *clientResources) gachaBanners() map[string]string {
	result := make(map[string]string, len(resources.Banners))
	for key, filename := range resources.Banners {
		if key != "" && key != "home" {
			result[key] = filename
		}
	}
	return result
}
