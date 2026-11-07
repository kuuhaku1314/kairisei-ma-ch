package cnbootstrap

import (
	"net/http"

	"kairisei.local/server/internal/httpapi"
	operationservice "kairisei.local/server/internal/operations"
)

func cnBootstrapHowToGetCardShow(business http.Handler, operations *operationservice.Runtime) http.HandlerFunc {
	return cnBootstrapExactBusiness(cnCardAcquisitionPublication(business, operations), "HowToGetCardShow", "/HowToGetCardShow", "cardids")
}

func cnCardAcquisitionPublication(business http.Handler, operations *operationservice.Runtime) http.Handler {
	return cnGachaPublicationBusiness(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		groups, err := operations.TeamBattleGroupAllowlist()
		if err != nil {
			http.Error(writer, "read local card acquisition publication", http.StatusInternalServerError)
			return
		}
		business.ServeHTTP(writer, httpapi.WithCardAcquisitionGroups(request, groups))
	}), operations)
}
