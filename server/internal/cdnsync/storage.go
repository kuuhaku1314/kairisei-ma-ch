package cdnsync

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"kairisei.local/server/internal/cnbootstrap"
	"kairisei.local/server/internal/objectstore"
)

func newHTTPClient() *http.Client { return objectstore.NewHTTPClient() }

func syncObject(ctx context.Context, storage *objectstore.Store, c config, object cnbootstrap.CDNObject) (bool, error) {
	key := object.Key
	if c.Prefix != "" {
		key = c.Prefix + "/" + key
	}
	contentType := "application/octet-stream"
	if strings.HasPrefix(object.Key, "image/") {
		contentType = "image/png"
	}
	uploaded, err := storage.Put(ctx, objectstore.Object{Key: key, SourcePath: object.SourcePath, Bytes: object.Bytes, SHA256: object.SHA256, ContentType: contentType})
	if errors.Is(err, objectstore.ErrConflict) {
		return false, errors.New("远端同名对象与本地版本不一致，已拒绝覆盖；请更新资源版本或 prefix")
	}
	return uploaded, err
}

func publicSamples(objects []cnbootstrap.CDNObject) []cnbootstrap.CDNObject {
	selected := map[string]cnbootstrap.CDNObject{objects[0].Key: objects[0]}
	for _, prefix := range []string{"patch/", "cpk/"} {
		var largest cnbootstrap.CDNObject
		for _, object := range objects {
			if strings.HasPrefix(object.Key, prefix) && object.Bytes > largest.Bytes {
				largest = object
			}
		}
		if largest.Key != "" {
			selected[largest.Key] = largest
		}
	}
	for _, object := range objects {
		if object.Key == "cpk/CPK/cv_navi_5.cpk.v2" {
			selected[object.Key] = object
		}
	}
	result := make([]cnbootstrap.CDNObject, 0, len(selected))
	for _, object := range objects {
		if _, ok := selected[object.Key]; ok {
			result = append(result, object)
		}
	}
	return result
}

func checkPublic(ctx context.Context, client *http.Client, base string, object cnbootstrap.CDNObject) error {
	address, err := objectstore.PublicURL(base, object.Key)
	if err != nil {
		return err
	}
	return objectstore.CheckPublic(ctx, client, address, objectstore.Object{Key: object.Key, SourcePath: object.SourcePath, Bytes: object.Bytes, SHA256: object.SHA256})
}
