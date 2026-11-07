package cnbootstrap

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/httpapi"
)

func (app *application) registerGameRoutes(router chi.Router) {
	router.Post("/Ping", cnBootstrapPing)
	router.Post("/Connect", cnBootstrapConnect(app.business))
	home := cnBootstrapSessionOnlyBusiness(app.business, "HomeShow", http.MethodPost, "/HomeShow")
	router.Post("/HomeShow", func(writer http.ResponseWriter, request *http.Request) {
		// Resolve publication only if Home needs to check a banner destination.
		var published map[int]struct{}
		var ready bool
		home(writer, httpapi.WithGachaPublication(request, func(id int) bool {
			if !ready {
				published, _ = app.operations.GachaPublication()
				ready = true
			}
			return app.operations.GachaIDPublished(id, published)
		}))
	})
	router.Post("/BattleRecoveryShow", cnBootstrapExactBusiness(app.business, "BattleRecoveryShow", "/BattleRecoveryShow", "abandoned_roomids"))
	router.Post("/PopupExec", cnBootstrapExactBusiness(app.business, "PopupExec", "/PopupExec", "popupid", "is_system"))
	router.Post("/TutorialProgress", cnBootstrapTutorialProgress)
	router.Post("/TutorialFlag", cnBootstrapTutorialFlag(app.business))
	router.Post("/ClickLog", cnBootstrapClickLog)
	router.Post("/ClickNewVersionInfo", cnBootstrapClickNewVersionInfo)
	router.Post("/UserCreate", cnBootstrapExactBusiness(app.business, "UserCreate", "/UserCreate", "name", "arthur_type"))
	router.Post("/UserSetName", cnBootstrapExactBusiness(app.business, "UserSetName", "/UserSetName", "name"))
	router.Post("/UserSetComment", cnBootstrapExactBusiness(app.business, "UserSetComment", "/UserSetComment", "comment"))
	router.Post("/GetMobileServiceToken", cnBootstrapExactBusiness(app.business, "GetMobileServiceToken", "/__domain/MobileServiceInfo", "sprite"))
	router.Post("/ItemShow", cnBootstrapItemShow(app.business))
	router.Post("/ItemUse", cnBootstrapExactBusiness(app.business, "ItemUse", "/ItemUse", "itemid"))
	router.Post("/ItemExchange", cnPayloadAdapter(app.business, "ItemExchange", "/ItemExchange", []string{"itemid", "change_sets"}, adaptCNNamedRewardMethod))
	router.Post("/ItemLackTips", cnBootstrapExactBusiness(app.business, "ItemLackTips", "/ItemLackTips", "idx"))
	router.Post("/ItemShopShow", cnBootstrapSessionOnlyBusiness(app.business, "ItemShopShow", http.MethodPost, "/ItemShopShow"))
	router.Post("/ItemShopBuy", cnBootstrapExactBusiness(app.business, "ItemShopBuy", "/ItemShopBuy", "item_shop_lineupid", "buy_num", "popupid"))
	router.Post("/EventShopShow", cnBootstrapExactBusiness(app.business, "EventShopShow", "/EventShopShow", "eventid"))
	router.Post("/EventShopBuy", cnBootstrapExactBusiness(app.business, "EventShopBuy", "/EventShopBuy", "event_shop_lineupid"))
	router.Post("/GachaShow", cnBootstrapGachaShow(app.business, app.operations))
	router.Post("/GachaPlay2", cnBootstrapGachaPlay(app.business, app.operations))
	router.Post("/GachaItemPlay", cnPayloadAdapter(app.business, "GachaItemPlay", "/GachaItemPlay", []string{"itemid", "play_count", "gacha_hash"}, adaptCNGachaItemPlayMethod))
	router.Post("/GachaLineupShow2", cnBootstrapGachaLineupShow(app.business, app.operations))
	gachaPublicationBusiness := cnGachaPublicationBusiness(app.business, app.operations)
	router.Post("/GachaSelectLineupShow", cnBootstrapExactBusiness(gachaPublicationBusiness, "GachaSelectLineupShow", "/GachaSelectLineupShow", "gachaid"))
	router.Post("/GachaSelectedListShow", cnBootstrapExactBusiness(gachaPublicationBusiness, "GachaSelectedListShow", "/GachaSelectedListShow", "gachaid"))
	router.Post("/GachaOddsShow", cnBootstrapGachaOddsShow(app.business, app.operations))
	router.Post("/CoinUse", cnBootstrapExactBusiness(app.business, "CoinUse", "/CoinUse", "type", "param"))
	router.Post("/TradeShopShow2", cnBootstrapSessionOnlyBusiness(app.business, "TradeShopShow2", http.MethodPost, "/TradeShopShow2"))
	router.Post("/TradeShopLineupShow", cnBootstrapExactBusiness(app.business, "TradeShopLineupShow", "/TradeShopLineupShow", "trade_shopid"))
	router.Post("/TradeShopBuy2", cnBootstrapTradeShopBuy2(app.business))
	router.Post("/PurchaseBonusActivityShow", cnBootstrapSessionOnlyBusiness(app.business, "PurchaseBonusActivityShow", http.MethodPost, "/PurchaseBonusActivityShow"))
	router.Post("/QueryOrder", cnBootstrapExactBusiness(app.business, "QueryOrder", "/QueryOrder", "order"))
	router.Post("/QueryCardCoin", cnBootstrapSessionOnlyBusiness(app.business, "QueryCardCoin", http.MethodPost, "/QueryCardCoin"))
	router.Post("/GetCardCoin", cnBootstrapExactBusiness(app.business, "GetCardCoin", "/GetCardCoin", "cardtype"))
	router.Post("/HonorShow", cnBootstrapHonorShow(app.business))
	router.Post("/HonorDeckShow", cnBootstrapSessionOnlyBusiness(app.business, "HonorDeckShow", http.MethodPost, "/HonorDeckShow"))
	router.Post("/HonorDeckSet", cnBootstrapExactBusiness(app.business, "HonorDeckSet", "/HonorDeckSet", "deck_honorids"))
	router.Post("/CardCollectionShow", cnBootstrapCardCollectionShow(app.business))
	router.Post("/CardShow2", cnBootstrapCardShow2(app.business))
	router.Post("/CardContainerShow", cnBootstrapCardContainerShow(app.business))
	router.Post("/CardMove", cnBootstrapCardMove(app.business))
	router.Post("/CardContainerLock", cnBootstrapExactBusiness(app.business, "CardContainerLock", "/CardContainerLock", "uniqid"))
	router.Post("/CardContainerUnlock", cnBootstrapExactBusiness(app.business, "CardContainerUnlock", "/CardContainerUnlock", "uniqid"))
	router.Post("/CardContainerSell", cnBootstrapCardContainerSell(app.business))
	router.Post("/CardLock", cnBootstrapExactBusiness(app.business, "CardLock", "/CardLock", "uniqid"))
	router.Post("/CardUnlock", cnBootstrapExactBusiness(app.business, "CardUnlock", "/CardUnlock", "uniqid"))
	router.Post("/CardLoveUp", cnBootstrapCardLoveUp(app.business))
	router.Post("/CardDecompose", cnBootstrapCardDecompose(app.business))
	router.Post("/CardFameTrainInfo", cnBootstrapSessionOnlyBusiness(app.business, "CardFameTrainInfo", http.MethodPost, "/CardFameTrainInfo"))
	router.Post("/CardFameStartTrain", cnBootstrapExactBusiness(app.business, "CardFameStartTrain", "/CardFameStartTrain", "base_uniqid", "base_fame"))
	router.Post("/CardFameCancelTrain", cnBootstrapExactBusiness(app.business, "CardFameCancelTrain", "/CardFameCancelTrain", "base_uniqid"))
	router.Post("/CardFameTrainFinish", cnBootstrapExactBusiness(app.business, "CardFameTrainFinish", "/CardFameTrainFinish", "base_uniqid"))
	router.Post("/HowToGetCardShow", cnBootstrapHowToGetCardShow(app.business, app.operations))
	router.Post("/CardCategoryGet", cnBootstrapCardCategoryGet(app.business))
	router.Post("/CardDeckSet", cnBootstrapCardDeckSet(app.business))
	router.Post("/SupportCardSlotUnlock", cnBootstrapExactBusiness(app.business, "SupportCardSlotUnlock", "/SupportCardSlotUnlock", "arthur_type"))
	router.Post("/CardFusion2", cnBootstrapCardFusion2(app.business))
	router.Post("/CardEvolution", cnBootstrapCardEvolution(app.business))
	router.Post("/CardSell", cnBootstrapCardSell(app.business))
	router.Post("/ExploreStart", cnBootstrapExploreStart(app.business))
	router.Post("/ExploreEnd", cnBootstrapExploreEnd(app.business))
	router.Post("/GiftCodeAd", cnBootstrapExactBusiness(app.business, "GiftCodeAd", "/GiftCodeAd", "channel", "codenumber"))
	router.Post("/MissionShow", cnBootstrapMissionShow(app.business))
	router.Post("/MissionReward", cnBootstrapMissionReward(app.business))
	router.Post("/MissionURLOpen", cnBootstrapExactBusiness(app.business, "MissionURLOpen", "/MissionURLOpen", "open_url"))
	router.Post("/PresentBoxShow", cnBootstrapPresentBoxShow(app.business))
	router.Post("/PresentBoxRecv", cnBootstrapPresentBoxRecv(app.business))
	router.Post("/PresentBoxMultiRecv2", cnBootstrapPresentBoxMultiRecv(app.business))
	router.Post("/PresentBoxDelete", cnBootstrapExactBusiness(app.business, "PresentBoxDelete", "/PresentBoxDelete", "presentid"))
	router.Post("/UpdGameOption", cnBootstrapExactBusiness(app.business, "UpdGameOption", "/UpdGameOption", "game_option"))
	router.Post("/UpdPushOption", cnBootstrapExactBusiness(app.business, "UpdPushOption", "/UpdPushOption", "option"))
	router.Post("/GetNaviShow", cnBootstrapSessionOnlyBusiness(app.business, "GetNaviShow", http.MethodPost, "/GetNaviShow"))
	router.Post("/NaviSelect", cnBootstrapExactBusiness(app.business, "NaviSelect", "/NaviSelect", "navi_type"))
	router.Post("/BuyNavi", cnBootstrapExactBusiness(app.business, "BuyNavi", "/BuyNavi", "navi_id"))
	router.Post("/StampShow", cnBootstrapSessionOnlyBusiness(app.business, "StampShow", http.MethodPost, "/StampShow"))
	router.Post("/StampDeckSet", cnBootstrapExactBusiness(app.business, "StampDeckSet", "/StampDeckSet", "deck"))
	router.Post("/CostumeShow", cnBootstrapSessionOnlyBusiness(app.business, "CostumeShow", http.MethodPost, "/CostumeShow"))
	router.Post("/CostumeSet", cnBootstrapExactBusiness(app.business, "CostumeSet", "/CostumeSet", "arthur_type", "costumeid"))
	router.Post("/AvatarPartsShow", cnBootstrapSessionOnlyBusiness(app.business, "AvatarPartsShow", http.MethodPost, "/AvatarPartsShow"))
	router.Post("/AvatarPartsDeckSet", cnBootstrapExactBusiness(app.business, "AvatarPartsDeckSet", "/AvatarPartsDeckSet", "avatar_parts_decks"))
	router.Post("/AvatarShopShow", cnBootstrapSessionOnlyBusiness(app.business, "AvatarShopShow", http.MethodPost, "/AvatarShopShow"))
	router.Post("/AvatarShopBuy", cnBootstrapExactBusiness(app.business, "AvatarShopBuy", "/AvatarShopBuy", "avatar_shop_lineupid", "sales_index"))
	router.Post("/DeckLimitShow", cnBootstrapSessionOnlyBusiness(app.business, "DeckLimitShow", http.MethodPost, "/DeckLimitShow"))
	router.Post("/TeamBattleSoloShow", cnBootstrapTeamBattleSoloShow(app.business, app.operations))
	router.Post("/TowerQuestShow", cnBootstrapExactBusiness(app.business, "TowerQuestShow", "/TowerQuestShow", "towerid"))
	router.Post("/TowerRankingShow", cnBootstrapExactBusiness(app.business, "TowerRankingShow", "/TowerRankingShow", "towerid"))
	router.Post("/TeamBattleSoloPartnerShow", cnBootstrapExactBusiness(app.business, "TeamBattleSoloPartnerShow", "/TeamBattleSoloPartnerShow", "bossid"))
	router.Post("/TeamBattleSoloPartnerRentalDeck", cnBootstrapExactBusiness(app.business, "TeamBattleSoloPartnerRentalDeck", "/TeamBattleSoloPartnerRentalDeck", "userid"))
	router.Post("/TeamBattleRecommendDeckShow", cnBootstrapSessionOnlyBusiness(app.business, "TeamBattleRecommendDeckShow", http.MethodPost, "/TeamBattleRecommendDeckShow"))
	router.Post("/TeamBattlePastBossShow", cnBootstrapPastBossShow(app.business, app.operations))
	router.Post("/TeamBattleClearDeckShow", cnBootstrapExactBusiness(cnCardAcquisitionPublication(app.business, app.operations), "TeamBattleClearDeckShow", "/TeamBattleClearDeckShow", "bossid"))
	router.Post("/TeamBattleScoreRewardLineup", cnBootstrapExactBusiness(app.business, "TeamBattleScoreRewardLineup", "/TeamBattleScoreRewardLineup", "bossid", "mode"))
	router.Post("/BossScoreRankingShow", cnBootstrapExactBusiness(app.business, "BossScoreRankingShow", "/BossScoreRankingShow", "bossid", "mode", "offset", "limit"))
	router.Post("/DailyClearRankShow", cnBootstrapExactBusiness(cnCardAcquisitionPublication(app.business, app.operations), "DailyClearRankShow", "/DailyClearRankShow", "bossid", "is_multi"))
	router.Post("/ChallengeShow", cnBootstrapExactBusiness(app.business, "ChallengeShow", "/ChallengeShow", "bossid", "target_time"))
	router.Post("/TeamBattleScheduleShow", cnBootstrapExactBusiness(app.business, "TeamBattleScheduleShow", "/TeamBattleScheduleShow", "is_solo", "active_arthur_type"))
	router.Post("/TeamBattleScheduleUpdate", cnBootstrapExactBusiness(app.business, "TeamBattleScheduleUpdate", "/TeamBattleScheduleUpdate", "is_solo", "boss_groupid"))
	router.Post("/UserBuffExec", cnBootstrapExactBusiness(app.business, "UserBuffExec", "/UserBuffExec", "user_buff_id"))
	router.Post("/TeamBattleSoloStart", cnBootstrapSoloStart(app.business))
	router.Post("/TeamBattleSoloContinue", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleSoloContinue",
		"/TeamBattleSoloContinue",
		"pay_type",
		"progress",
		"input_cmd",
		"enemy_dead_bit",
	))
	router.Post("/TeamBattleSoloEnd", cnBootstrapTeamBattleSoloEnd(app.business))
	router.Post("/TeamBattleSkip", cnPayloadAdapter(app.business, "TeamBattleSkip", "/TeamBattleSkip", []string{"bossid", "num"}, adaptCNNamedRewardMethod))
	router.Post("/TeamBattleMultiShow", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleMultiShow",
		"/TeamBattleMultiShow",
		"active_arthur_type",
	))
	router.Post("/TeamBattleMultiRoomSearch", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleMultiRoomSearch",
		"/TeamBattleMultiRoomSearch",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"pass",
		"is_rookie",
		"bossid",
		"boss_groupid",
		"rookie_type",
		"searchid",
		"quest_get_time",
		"is_auto",
		"empty_time",
	))
	router.Post("/TeamBattleMultiRoomCreate", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleMultiRoomCreate",
		"/TeamBattleMultiRoomCreate",
		"bossid",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"room_type",
		"pass",
		"deck_rank",
		"hp",
		"fame",
		"comment",
		"allow_to_leave",
		"use_punished_free_piont",
		"is_battle_msg_skip",
		"battle_speed_rate",
	))
	router.Post("/TeamBattleAIRoomCreate", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleAIRoomCreate",
		"/TeamBattleAIRoomCreate",
		"bossid",
		"ai_id",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"use_punished_free_piont",
	))
	router.Post("/TeamBattleMultiRoomReserve", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleMultiRoomReserve",
		"/TeamBattleMultiRoomReserve",
		"roomid",
		"deck_arthur_type",
	))
	router.Post("/TeamBattleMultiRoomReserveCancel", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleMultiRoomReserveCancel",
		"/TeamBattleMultiRoomReserveCancel",
		"roomid",
		"deck_arthur_type",
	))
	router.Post("/TeamBattleMultiRoomEnter", cnBootstrapExactBusiness(
		app.business,
		"TeamBattleMultiRoomEnter",
		"/TeamBattleMultiRoomEnter",
		"roomid",
		"deck_arthur_type",
		"deck_arthur_type_idx",
		"use_punished_free_piont",
	))
	router.Post("/TeamBattleResult", cnBootstrapTeamBattleResult(app.business))
	router.Post("/MainQuestShow", cnBootstrapSessionOnlyBusiness(app.business, "MainQuestShow", http.MethodPost, "/MainQuestShow"))
	router.Post("/SphrShow", cnBootstrapSphereShow(app.business))
	router.Post("/SphrFusion2", cnBootstrapSphereFusion2(app.business))
	router.Post("/SphrEvolution", cnBootstrapSphereEvolution(app.business))
	router.Post("/SphrSell", cnBootstrapSphereSell(app.business))
	router.Post("/SphrLock", cnBootstrapSphereLock(app.business, false))
	router.Post("/SphrUnlock", cnBootstrapSphereLock(app.business, true))
	router.Post("/BuddyShow", cnBootstrapBuddyShow(app.business))
	router.Post("/BuddyFusion", cnBootstrapBuddyFusion(app.business))
	router.Post("/BuddyEvolution", cnBootstrapBuddyEvolution(app.business))
	router.Post("/BuddySell", cnBootstrapBuddySell(app.business))
	router.Post("/BuddyLock", cnBootstrapBuddyLock(app.business, false))
	router.Post("/BuddyUnlock", cnBootstrapBuddyLock(app.business, true))
	router.Post("/FollowoFollowShow", cnBootstrapFollowShow(app.business))
	router.Post("/FollowFollowerShow", cnBootstrapFollowerShow(app.business))
	router.Post("/FriendSearch", cnBootstrapFriendSearch(app.business))
	router.Post("/FollowAdd", cnBootstrapFollowAdd(app.business))
	router.Post("/FollowUnFollow", cnBootstrapFollowUnfollow(app.business))
	router.Post("/UserProfileShow", cnBootstrapUserProfileShow(app.business))
	router.Post("/StoryMainShow", cnBootstrapStoryMainShow(app.business))
	router.Post("/StoryMainStart", cnBootstrapStoryMainStart(app.business))
	router.Post("/StoryMainEnd", cnBootstrapStoryMainEnd(app.business))
	router.Post("/StorySubShow", cnBootstrapStorySubShow(app.business))
	router.Post("/StorySubStart", cnBootstrapStorySubStart(app.business))
	router.Post("/StorySubEnd", cnBootstrapStorySubEnd(app.business))
	router.Post("/StoryEventShow", cnBootstrapStoryEventShow(app.business))
	router.Post("/StoryEventUnlock", cnBootstrapStoryEventUnlock(app.business))
	router.Post("/StoryStart", cnBootstrapStoryStart(app.business))
	router.Post("/StoryTeamBattleStart", cnBootstrapExactBusiness(app.business,
		"StoryTeamBattleStart", "/StoryTeamBattleStart", "story_teambattleid"))
	router.Post("/StoryTeamBattleEnd", cnBootstrapStoryTeamBattleEnd(app.business))
	router.Post("/EventShow", cnBootstrapSessionOnlyBusiness(app.business, "EventShow", http.MethodPost, "/EventShow"))
	router.Post("/PvpShow", cnBootstrapSessionOnlyBusiness(app.business, "PvpShow", http.MethodPost, "/PvpShow"))
	router.Post("/PvpStart2", app.admitPVPStart(cnBootstrapExactBusiness(app.business, "PvpStart2", "/PvpStart2", "type", "select_arthur_type", "pvp_my_deck")))
	router.Post("/PvpEnd", cnBootstrapPvpEnd(app.business))
}
