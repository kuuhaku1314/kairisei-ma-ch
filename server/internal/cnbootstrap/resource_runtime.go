package cnbootstrap

import "kairisei.local/server/internal/cpk"

// clientResources is a validated, read-only inventory shared by every account.
type clientResources struct {
	GachaBanner         string
	FiveStarGachaBanner string
	HomeBanner          string
	GachaBanners        map[string]string
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
	absoluteGachaBanner, err := resolveBanner(config.GachaBanner, "gacha")
	if err != nil {
		return nil, err
	}
	absoluteFiveStarGachaBanner, err := resolveBanner(config.FiveStarGachaBanner, "five-star gacha")
	if err != nil {
		return nil, err
	}
	absoluteHomeBanner, err := resolveBanner(config.HomeBanner, "Home")
	if err != nil {
		return nil, err
	}
	gachaBannerPaths, err := resolveCN602GachaBannerPaths(absoluteGachaBanner, absoluteFiveStarGachaBanner)
	if err != nil {
		return nil, err
	}
	bannerVersions, err := gachaBannerVersions(absoluteGachaBanner, gachaBannerPaths)
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
		GachaBanner:         absoluteGachaBanner,
		FiveStarGachaBanner: absoluteFiveStarGachaBanner,
		HomeBanner:          absoluteHomeBanner,
		GachaBanners:        gachaBannerPaths,
		GachaBannerVersions: bannerVersions,
		CPK:                 cpkDelivery,
		Images:              images,
		CPKList:             cpkFileList,
		CPKState:            cpkPatchState,
		AvailableBundles:    platforms.availableBundles(),
		Platforms:           platforms,
	}, nil
}
