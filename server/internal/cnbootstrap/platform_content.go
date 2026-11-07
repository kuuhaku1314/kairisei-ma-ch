package cnbootstrap

// platformContentObject is one public download and its validated source.
// Only profile loading writes this inventory. HTTP and CDN export consume the
// same names, pins and source ownership; account and operator state stay outside.
type platformContentObject struct {
	file             platformDeliveryFile
	sourceRoot, root string
	contentType      string
	data             []byte
}

type platformDelivery struct {
	platform, unityVersion, contentID string
	profileSHA                        string
	objects                           map[string]platformContentObject
	bundles                           map[string]platformDeliveryFile
}

type platformDeliveries map[string]*platformDelivery

// Account catalogs are shared across configured targets. Only publish assets
// available on every target, including owners carried inside transport packs.
func (set platformDeliveries) availableBundles() map[string]struct{} {
	var available map[string]struct{}
	for _, delivery := range set {
		if available == nil {
			available = make(map[string]struct{}, len(delivery.bundles))
			for name := range delivery.bundles {
				available[name] = struct{}{}
			}
			continue
		}
		for name := range available {
			if _, exists := delivery.bundles[name]; !exists {
				delete(available, name)
			}
		}
	}
	return available
}

func platformKey(platform, unity string) string { return platform + "\x00" + unity }

func (set platformDeliveries) selectDelivery(platform, unity, content string) *platformDelivery {
	delivery := set[platformKey(platform, unity)]
	if delivery == nil || (content != "current" && content != delivery.contentID) {
		return nil
	}
	return delivery
}

func (delivery *platformDelivery) registerMovies(movies *platformMovieDelivery) {
	delivery.objects["movies/index.json"] = platformContentObject{
		file:       platformDeliveryFile{path: movies.indexPath, size: int64(len(movies.index)), sha: movies.indexSHA},
		sourceRoot: "movie-index", root: movies.indexRoot,
		contentType: "application/json", data: movies.index,
	}
	for name, file := range movies.files {
		delivery.objects[name] = platformContentObject{
			file: file, sourceRoot: "movie-artifacts", root: movies.artifactRoot,
			contentType: "video/mp4",
		}
	}
}
