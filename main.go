package main

import (
	"cuento-backend/src/Controllers"
	"cuento-backend/src/EventHandlers"
	"cuento-backend/src/Events"
	"cuento-backend/src/Features"
	"cuento-backend/src/MCP"
	"cuento-backend/src/Middlewares"
	"cuento-backend/src/Router"
	"cuento-backend/src/Services"
	"cuento-backend/src/Websockets"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	Services.InitDB()
	Services.InitSonic()
	Services.InitQdrant(Services.DB)
	Services.QueueNotifyFunc = MCP.NotifyWorker
	if err := Services.InitI18n("locales"); err != nil {
		panic("failed to load i18n bundles: " + err.Error())
	}
	EventHandlers.RegisterEventHandlers(Services.DB)

	// Start health monitor (RAM stats every 30s, one log file per day, 30-day retention)
	Controllers.InitHealthBroadcaster()
	Controllers.InitUserRefreshCallbacks()
	Services.InitHealthMonitor()

	// Start archiving warning notifier (checks daily, sends notifications at 10/5/3/2/1 days before archiving)
	Services.StartArchivingNotifier(Services.DB)

	// Start WebSocket Hub
	go Websockets.MainHub.Run()

	// Start MCP Server
	go func() {
		if err := MCP.StartMCPServer(Services.DB, ":8081"); err != nil {
			fmt.Printf("MCP server error: %v\n", err)
		}
	}()

	// Evict users inactive for more than 10 minutes from the activity list
	// Clean up stale guest fingerprints every 5 minutes
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			Services.GuestActivity.Cleanup()
		}
	}()

	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			evicted := Services.ActivityStorage.EvictInactiveUsers(10 * time.Minute)
			if len(evicted) > 0 {
				for _, userID := range evicted {
					Events.Publish(Services.DB, Events.UserActivityChanged, Events.UserActivityChangedEvent{UserID: userID})
				}
				Controllers.BroadcastActiveUserActivity(Services.DB)
				Controllers.BroadcastActiveUsersToHome()
			}
		}
	}()

	r := gin.Default()
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowHeaders = []string{"Origin", "Content-Length", "Content-Type", "Authorization", "X-Screen-Resolution", "Sec-CH-UA"}
	r.Use(cors.New(config))

	// Apply error middleware globally
	r.Use(Middlewares.ErrorMiddleware())
	r.Use(Middlewares.FeatureFlagsMiddleware(Services.DB))
	r.Use(Middlewares.GuestTrackingMiddleware())

	// Public routes
	publicRouter := Router.NewCustomRouter(r.Group("/"))

	publicRouter.GET("/episode/:id/warnings/:locale", "Get warnings for an episode filtered by locale", func(c *gin.Context) {
		Controllers.GetEpisodeWarnings(c, Services.DB)
	})

	// User routes (Public)
	publicRouter.GET("/search/buckets", "Get list of available search buckets", func(c *gin.Context) {
		Controllers.GetSonicBuckets(c)
	})
	publicRouter.POST("/register", "Register a new user account", func(c *gin.Context) {
		Controllers.Register(c, Services.DB)
	})
	publicRouter.POST("/login", "Login with user credentials", func(c *gin.Context) {
		Controllers.Login(c, Services.DB)
	})
	publicRouter.POST("/refresh", "Refresh access token", func(c *gin.Context) {
		Controllers.RefreshToken(c, Services.DB)
	})
	publicRouter.GET("/currency/settings", "Get currency settings", func(c *gin.Context) {
		Features.GetCurrencySettingsHandler(c, Services.DB)
	})
	publicRouter.GET("/board/info", "Get board information", func(c *gin.Context) {
		Controllers.GetBoard(c, Services.DB)
	})
	publicRouter.GET("/panel/:key/content", "Get rendered panel content by key", func(c *gin.Context) {
		Controllers.GetPanelContentByName(c, Services.DB)
	})
	publicRouter.GET("/widget/:id/render", "Render a widget by ID", func(c *gin.Context) {
		Controllers.RenderWidget(c, Services.DB)
	})
	publicRouter.GET("/entity/fields/:entity_type", "Get field names for an entity type", func(c *gin.Context) {
		Controllers.GetEntityFields(c, Services.DB)
	})
	publicRouter.GET("/smiles", "Get smile categories with their smiles", func(c *gin.Context) {
		Controllers.GetSmileTree(c, Services.DB)
	})
	publicRouter.GET("/user/profile/:userID", "Get user profile details", func(c *gin.Context) {
		Controllers.GetUserProfile(c, Services.DB)
	})
	publicRouter.GET("/user/list", "Get list of active users and their characters", func(c *gin.Context) {
		Controllers.GetUserList(c, Services.DB)
	})
	publicRouter.GET("/absent-users", "Get currently absent users with their return date", func(c *gin.Context) {
		Controllers.GetAbsentUsers(c, Services.DB)
	})

	publicRouter.GET("/user/recent", "Get users active in the past 24 hours", func(c *gin.Context) {
		Controllers.GetRecentActiveUsers(c, Services.DB)
	})
	publicRouter.GET("/character/recent", "Get characters of users active in the past 24 hours", func(c *gin.Context) {
		Controllers.GetRecentActiveCharacters(c, Services.DB)
	})
	publicRouter.GET("/user/autocomplete/:term", "Get users matching search term", func(c *gin.Context) {
		Controllers.UserAutocomplete(c, Services.DB)
	})
	publicRouter.POST("/recovery", "Retrieve private key by recovery code hash", func(c *gin.Context) {
		Controllers.Recovery(c, Services.DB)
	})
	publicRouter.POST("/update-password", "Update user password via recovery flow", func(c *gin.Context) {
		Controllers.UpdatePassword(c, Services.DB)
	})
	wipeRateLimiter := Middlewares.NewRateLimiter(5, time.Hour)
	r.POST("/user/wipe", wipeRateLimiter.Middleware(), func(c *gin.Context) {
		Controllers.WipeOutMyUser(c, Services.DB)
	})
	publicRouter.GET("/character/field-list/:machine_name", "Get distinct values of a string character custom field", func(c *gin.Context) {
		Controllers.CustomFieldList(c, Services.DB)
	})
	publicRouter.GET("/character/get/:id", "Get character details by ID", func(c *gin.Context) {
		Controllers.GetCharacter(c, Services.DB)
	})
	publicRouter.GET("/character-profile/get/:id", "Get character profile details by ID", func(c *gin.Context) {
		Controllers.GetCharacterProfile(c, Services.DB)
	})
	publicRouter.GET("/wanted-character/field-list/:machine_name", "Get distinct values of a string wanted character custom field", func(c *gin.Context) {
		Controllers.WantedCustomFieldList(c, Services.DB)
	})
	publicRouter.POST("/wanted-character/list", "Get list of unclaimed wanted characters", func(c *gin.Context) {
		Controllers.GetWantedCharacterList(c, Services.DB)
	})
	publicRouter.GET("/wanted-character/tree-list", "Get faction tree with unclaimed wanted characters", func(c *gin.Context) {
		Controllers.GetWantedCharacterTreeList(c, Services.DB)
	})
	publicRouter.GET("/wanted-character/get/:id", "Get wanted character details by ID", func(c *gin.Context) {
		Controllers.GetWantedCharacter(c, Services.DB)
	})
	publicRouter.GET("/factions/get/wanted", "Get faction tree filtered to factions with active wanted characters", func(c *gin.Context) {
		Controllers.GetWantedFactionTree(c, Services.DB)
	})

	// Optional Auth routes (Context populated if token present, otherwise Guest)
	optionalAuthGroup := r.Group("/")
	optionalAuthGroup.Use(Middlewares.OptionalAuthMiddleware())
	optionalAuthRouter := Router.NewProtectedCustomRouter(optionalAuthGroup)
	optionalAuthRouter.GET("/global-settings", "Get all global settings", func(c *gin.Context) {
		Controllers.GetGlobalSettings(c, Services.DB)
	})
	optionalAuthRouter.GET("/categories/home", "Get home page categories", func(c *gin.Context) {
		Controllers.GetHomeCategories(c, Services.DB)
	})
	optionalAuthRouter.GET("/active-users", "Get currently active users", func(c *gin.Context) {
		Controllers.GetActiveUsers(c)
	})
	optionalAuthRouter.GET("/active-users/activity", "Get full activity info for active users", func(c *gin.Context) {
		Controllers.GetActiveUserActivity(c, Services.DB)
	})
	publicRouter.POST("/guest/activity", "Update guest location for active users list", func(c *gin.Context) {
		Controllers.UpdateGuestLocation(c)
	})
	optionalAuthRouter.GET("/search", "Search across buckets", func(c *gin.Context) {
		Controllers.Search(c, Services.DB)
	})
	optionalAuthRouter.GET("/search/count", "Get result count for a search query", func(c *gin.Context) {
		Controllers.SearchCount(c, Services.DB)
	})
	optionalAuthRouter.GET("/ping", "Health check endpoint", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})
	optionalAuthRouter.GET("/viewforum/:subforum/:page", "Get topics in a subforum by page", func(c *gin.Context) {
		Controllers.GetTopicsBySubforum(c, Services.DB)
	})
	optionalAuthRouter.GET("/topic/get/:id", "Get topic details by ID", func(c *gin.Context) {
		Controllers.GetTopic(c, Services.DB)
	})
	optionalAuthRouter.GET("/character-list", "Get list of all characters", func(c *gin.Context) {
		Controllers.GetCharacterList(c, Services.DB)
	})
	optionalAuthRouter.GET("/subforum/list-short", "Get list of all subforums", func(c *gin.Context) {
		Controllers.GetShortSubforumList(c, Services.DB)
	})
	optionalAuthRouter.GET("/character-autocomplete/:term", "Get list of characters matching search term", func(c *gin.Context) {
		Controllers.GetCharacterAutocomplete(c, Services.DB)
	})
	optionalAuthRouter.GET("/episode-autocomplete/:term", "Get list of episodes matching search term, optionally filtered to current user's episodes", func(c *gin.Context) {
		Controllers.GetEpisodeAutocomplete(c, Services.DB)
	})
	optionalAuthRouter.GET("/mask-autocomplete/:term", "Get list of masks matching search term", func(c *gin.Context) {
		Controllers.GetMaskAutocomplete(c, Services.DB)
	})
	optionalAuthRouter.GET("/wanted-character-autocomplete/:term", "Get list of wanted characters matching search term", func(c *gin.Context) {
		Controllers.GetWantedCharacterAutocomplete(c, Services.DB)
	})
	optionalAuthRouter.GET("/claim-autocomplete/:term", "Get list of claims not linked to wanted characters matching search term", func(c *gin.Context) {
		Controllers.GetClaimAutocomplete(c, Services.DB)
	})
	optionalAuthRouter.POST("/claim-record/create", "Create a new claim record for a wanted character or claim", func(c *gin.Context) {
		Controllers.CreateClaimRecord(c, Services.DB)
	})
	optionalAuthRouter.POST("/claim-record/revoke", "Revoke an active claim record", func(c *gin.Context) {
		Controllers.RevokeClaim(c, Services.DB)
	})
	optionalAuthRouter.POST("/faction/create-pending", "Create a new faction in pending status", func(c *gin.Context) {
		Controllers.CreatePendingFaction(c, Services.DB)
	})
	optionalAuthRouter.POST("/role-claim/create", "Create a new role claim with character name and faction", func(c *gin.Context) {
		Controllers.CreateNewRoleClaim(c, Services.DB)
	})
	optionalAuthRouter.GET("/factions/get", "Get faction tree", func(c *gin.Context) {
		Controllers.GetActiveFactionTree(c, Services.DB)
	})
	publicRouter.GET("/faction-children/:parent_id/get", "Get child factions by parent ID", func(c *gin.Context) {
		Controllers.GetFactionChildren(c, Services.DB)
	})
	publicRouter.GET("/draft/:session_key/main_style.css", "Get main CSS content for a design draft by session key", func(c *gin.Context) {
		Controllers.GetDesignDraftMainCss(c, Services.DB)
	})
	publicRouter.GET("/draft/:session_key/custom_style.css", "Get custom style CSS content for a design draft by session key", func(c *gin.Context) {
		Controllers.GetDesignDraftCustomStyleCss(c, Services.DB)
	})
	optionalAuthRouter.POST("/episodes/get", "Get episode list", func(c *gin.Context) {
		Controllers.GetEpisodes(c, Services.DB)
	})
	optionalAuthRouter.POST("/episodes/get-by-mask", "Get episode list for a mask", func(c *gin.Context) {
		Controllers.GetEpisodesByMask(c, Services.DB)
	})
	optionalAuthRouter.GET("/subforum/get/:id", "Get subforum details by ID", func(c *gin.Context) {
		Controllers.GetSubforum(c, Services.DB)
	})
	optionalAuthRouter.GET("/topic-posts/:id", "Get posts in a topic by page", func(c *gin.Context) {
		Controllers.GetPostsByTopic(c, Services.DB)
	})
	optionalAuthRouter.GET("/users/page/:page_type/:page_id", "Get users currently viewing a page", func(c *gin.Context) {
		Controllers.GetUsersByPage(c, Services.DB)
	})
	optionalAuthRouter.GET("/user/character-profiles-topic/:topicID", "Get current user's character profiles for a topic", func(c *gin.Context) {
		Controllers.GetCharacterProfilesByUserAndTopic(c, Services.DB)
	})
	optionalAuthRouter.POST("/post/create", "Create a new post in a topic", func(c *gin.Context) {
		Controllers.CreatePost(c, Services.DB)
	})
	optionalAuthRouter.POST("/post/preview", "Preview a post without saving", func(c *gin.Context) {
		Controllers.PreviewPost(c, Services.DB)
	})
	optionalAuthRouter.GET("/active-topics", "Get list of active topics", func(c *gin.Context) {
		Controllers.GetActiveTopics(c, Services.DB)
	})
	optionalAuthRouter.GET("/active-topic-count", "Get count of active topics", func(c *gin.Context) {
		Controllers.GetActiveTopicCount(c, Services.DB)
	})
	optionalAuthRouter.GET("/mask/:id", "Get mask by ID", func(c *gin.Context) {
		Controllers.GetMask(c, Services.DB)
	})
	optionalAuthRouter.GET("/user-masks/:userID", "Get user's masks", func(c *gin.Context) {
		Controllers.GetUserMasks(c, Services.DB)
	})
	optionalAuthRouter.GET("/post/:id", "Get post by ID", func(c *gin.Context) {
		Controllers.GetPostById(c, Services.DB)
	})
	optionalAuthRouter.GET("/additional-navlink/list", "Get additional navlinks visible to the current user", func(c *gin.Context) {
		Controllers.GetAdditionalNavlinkListByUser(c, Services.DB)
	})

	// Auth-only routes (JWT required, no per-route permission check — controller handles authorization)
	authOnlyGroup := r.Group("/")
	authOnlyGroup.Use(Middlewares.AuthMiddleware())
	authOnlyRouter := Router.NewCustomRouter(authOnlyGroup)

	authOnlyRouter.GET("/admin/backup", "Download a full SQL backup of the database", func(c *gin.Context) {
		Controllers.BackupDB(c)
	})
	authOnlyRouter.POST("/admin/backup/restore", "Restore the database from an uploaded SQL file", func(c *gin.Context) {
		Controllers.RestoreDB(c)
	})

	// Protected routes
	protectedGroup := r.Group("/")
	protectedGroup.Use(Middlewares.AuthMiddleware())
	protectedGroup.Use(Middlewares.PermissionsMiddleware(Services.DB))
	protectedRouter := Router.NewProtectedCustomRouter(protectedGroup)

	optionalAuthRouter.GET("/features", "Get list of all feature flags", func(c *gin.Context) {
		Features.GetFeaturesHandler(c)
	})
	protectedRouter.POST("/features/:key/toggle", "Toggle a feature flag on or off", func(c *gin.Context) {
		Features.ToggleFeatureHandler(c, Services.DB)
	})
	protectedRouter.GET("/currency/income-types", "Get list of currency income types", func(c *gin.Context) {
		Features.GetCurrencyIncomeTypesHandler(c, Services.DB)
	})
	protectedRouter.GET("/currency/active-income-types", "Get list of active currency income types", func(c *gin.Context) {
		Features.GetActiveCurrencyIncomeTypesHandler(c, Services.DB)
	})
	protectedRouter.POST("/currency/settings/update", "Update currency settings", func(c *gin.Context) {
		Features.UpdateCurrencySettingsHandler(c, Services.DB)
	})
	protectedRouter.POST("/currency/income-types/update", "Update currency income types", func(c *gin.Context) {
		Features.UpdateCurrencyIncomeTypesHandler(c, Services.DB)
	})
	protectedRouter.GET("/currency/spend-types", "Get list of currency spend types", func(c *gin.Context) {
		Features.GetCurrencySpendTypesHandler(c, Services.DB)
	})
	protectedRouter.GET("/currency/active-spend-types", "Get list of active currency spend types", func(c *gin.Context) {
		Features.GetActiveCurrencySpendTypesHandler(c, Services.DB)
	})
	protectedRouter.POST("/currency/spend-types/update", "Update currency spend types", func(c *gin.Context) {
		Features.UpdateCurrencySpendTypesHandler(c, Services.DB)
	})
	protectedRouter.POST("/post-top/create", "Create a post top", func(c *gin.Context) {
		Features.CreatePostTopHandler(c, Services.DB)
	})
	protectedRouter.POST("/post-top/:id/update", "Update a post top", func(c *gin.Context) {
		Features.UpdatePostTopHandler(c, Services.DB)
	})
	protectedRouter.GET("/post-top/:id", "Get post top", func(c *gin.Context) {
		Features.GetPostTopHandler(c, Services.DB)
	})
	protectedRouter.GET("/currency/user/amount", "Get current user's currency amount", func(c *gin.Context) {
		Features.GetUserCurrencyAmountHandler(c, Services.DB)
	})
	protectedRouter.GET("/currency/user/:user_id/transactions", "Get user's currency transactions", func(c *gin.Context) {
		Features.GetUserCurrencyTransactionsHandler(c, Services.DB)
	})
	protectedRouter.POST("/currency/user/:user_id/transactions/add", "Add a currency transaction for a user", func(c *gin.Context) {
		Features.AddUserCurrencyTransactionHandler(c, Services.DB)
	})
	protectedRouter.POST("/character/create", "Create a new character", func(c *gin.Context) {
		Controllers.CreateCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/preview", "Preview a character without saving", func(c *gin.Context) {
		Controllers.PreviewCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/update/:id", "Update character by ID", func(c *gin.Context) {
		Controllers.UpdateCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/:id/avatar", "Upload and save a character's avatar", func(c *gin.Context) {
		Controllers.UploadCharacterAvatar(c, Services.DB)
	})
	protectedRouter.GET("/user/characters", "Get current user's characters", func(c *gin.Context) {
		Controllers.GetUserCharacters(c, Services.DB)
	})
	protectedRouter.GET("/user/character-profiles", "Get current user's character profiles", func(c *gin.Context) {
		Controllers.GetCharacterProfilesByUser(c, Services.DB)
	})
	protectedRouter.GET("/faction-tree", "Get faction tree by ID", func(c *gin.Context) {
		Controllers.GetFactionTree(c, Services.DB)
	})
	protectedRouter.POST("/faction/create", "Create a new faction", func(c *gin.Context) {
		Controllers.CreateFaction(c, Services.DB)
	})
	protectedRouter.GET("/factions/pending", "Get pending factions", func(c *gin.Context) {
		Controllers.GetPendingFactions(c, Services.DB)
	})
	protectedRouter.POST("/faction/update/:id", "Update faction by ID", func(c *gin.Context) {
		Controllers.UpdateFactionById(c, Services.DB)
	})
	protectedRouter.GET("/faction/delete/:id", "Delete faction by ID", func(c *gin.Context) {
		Controllers.DeleteFaction(c, Services.DB)
	})
	protectedRouter.POST("/global-settings/update", "Update global settings", func(c *gin.Context) {
		Controllers.UpdateGlobalSettings(c, Services.DB)
	})
protectedRouter.GET("/character-claims", "Get list of all character claims grouped by faction", func(c *gin.Context) {
		Controllers.GetCharacterClaims(c, Services.DB)
	})
	protectedRouter.POST("/character-claim/create", "Create a new character claim", func(c *gin.Context) {
		Controllers.CreateCharacterClaim(c, Services.DB)
	})
	protectedRouter.POST("/character-claim/delete/:id", "Delete a character claim", func(c *gin.Context) {
		Controllers.DeleteCharacterClaim(c, Services.DB)
	})

	// Character Template routes
	publicRouter.GET("/template/episode/get", "Get episode template (public)", func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "type", Value: "episode"})
		Controllers.GetTemplate(c, Services.DB)
	})
	publicRouter.GET("/template/wanted_character/get", "Get wanted character template (public)", func(c *gin.Context) {
		c.Params = append(c.Params, gin.Param{Key: "type", Value: "wanted_character"})
		Controllers.GetTemplate(c, Services.DB)
	})
	publicRouter.GET("/custom-field/autocomplete", "Autocomplete distinct string custom field values", func(c *gin.Context) {
		Controllers.CustomFieldAutocomplete(c, Services.DB)
	})
	protectedRouter.GET("/template/:type/get", "Get character template by type", func(c *gin.Context) {
		Controllers.GetTemplate(c, Services.DB)
	})
	protectedRouter.POST("/template/:type/update", "Update character template by type", func(c *gin.Context) {
		Controllers.UpdateTemplate(c, Services.DB)
	})
	protectedRouter.POST("/episode/create", "Create a new episode", func(c *gin.Context) {
		Controllers.CreateEpisode(c, Services.DB)
	})
	protectedRouter.POST("/wanted-character/create", "Create a new wanted character", func(c *gin.Context) {
		Controllers.CreateWantedCharacter(c, Services.DB)
	})
	protectedRouter.POST("/wanted-character/update/:id", "Update a wanted character by ID", func(c *gin.Context) {
		Controllers.UpdateWantedCharacter(c, Services.DB)
	})
	protectedRouter.POST("/episode/preview", "Preview an episode without saving", func(c *gin.Context) {
		Controllers.PreviewEpisode(c, Services.DB)
	})
	protectedRouter.POST("/episode/update/:id", "Update episode by ID", func(c *gin.Context) {
		Controllers.UpdateEpisode(c, Services.DB)
	})
	protectedRouter.GET("/permission-matrix/get", "Get permission matrix", func(c *gin.Context) {
		Controllers.GetPermissionMatrix(c, Services.DB)
	})
	protectedRouter.POST("/permission-matrix/update", "Update permission matrix", func(c *gin.Context) {
		Controllers.UpdatePermissionMatrix(c, Services.DB)
	})
	protectedRouter.POST("/post/update/:id", "Update post by ID", func(c *gin.Context) {
		Controllers.UpdatePost(c, Services.DB)
	})
	protectedRouter.POST("/post/delete/:id", "Soft-delete a post by ID", func(c *gin.Context) {
		Controllers.DeletePost(c, Services.DB)
	})
	protectedRouter.POST("/character-profile/update/:id", "Update character profile by ID", func(c *gin.Context) {
		Controllers.CharacterProfileUpdate(c, Services.DB)
	})
	protectedRouter.POST("/character-profile/:id/avatar", "Upload and save a character profile's avatar", func(c *gin.Context) {
		Controllers.UploadCharacterProfileAvatar(c, Services.DB)
	})
	protectedRouter.POST("/topic/create", "Create topic", func(c *gin.Context) {
		Controllers.CreateTopic(c, Services.DB)
	})
	protectedRouter.POST("/topic/update/:id", "Update topic by ID", func(c *gin.Context) {
		Controllers.UpdateTopic(c, Services.DB)
	})
	publicRouter.GET("/lore-topic/:id/pages", "Get lore pages by topic ID", func(c *gin.Context) {
		Controllers.GetLorePagesByTopic(c, Services.DB)
	})
	protectedRouter.GET("/lore-topic/:id/posts", "Get posts of a lore topic with lore page data", func(c *gin.Context) {
		Controllers.GetLoreTopicPosts(c, Services.DB)
	})
	protectedRouter.POST("/lore-topic/create", "Create lore topic", func(c *gin.Context) {
		Controllers.CreateLoreTopic(c, Services.DB)
	})
	protectedRouter.POST("/lore-topic/update/:id", "Update lore topic by ID", func(c *gin.Context) {
		Controllers.UpdateLoreTopic(c, Services.DB)
	})
	protectedRouter.POST("/lore-page/create", "Create a lore page", func(c *gin.Context) {
		Controllers.CreateLorePage(c, Services.DB)
	})
	protectedRouter.POST("/lore-page/update/:post_id", "Update lore page by post ID", func(c *gin.Context) {
		Controllers.UpdateLorePage(c, Services.DB)
	})
	protectedRouter.GET("/lore-page/delete/:post_id", "Delete lore page by post ID", func(c *gin.Context) {
		Controllers.DeleteLorePage(c, Services.DB)
	})
	protectedRouter.POST("/topics/move", "Move topics to a different subforum", func(c *gin.Context) {
		Controllers.MoveTopics(c, Services.DB)
	})
	protectedRouter.POST("/topics/bulk-update", "Bulk update topics", func(c *gin.Context) {
		Controllers.BulkUpdateTopics(c, Services.DB)
	})
	protectedRouter.POST("/admin/topics/delete", "Batch delete topics (sets status to deleted)", func(c *gin.Context) {
		Controllers.BatchDeleteTopics(c, Services.DB)
	})
	publicRouter.GET("/notifications/types", "Get list of notification types", func(c *gin.Context) {
		Controllers.GetNotificationTypes(c)
	})
	protectedRouter.GET("/notifications/unread", "Get unread notifications for the current user", func(c *gin.Context) {
		Controllers.GetUnreadNotifications(c, Services.DB)
	})
	protectedRouter.GET("/notifications/settings", "Get notification settings for the current user", func(c *gin.Context) {
		Controllers.GetNotificationSettings(c, Services.DB)
	})
	protectedRouter.POST("/notifications/settings/update", "Update a notification setting", func(c *gin.Context) {
		Controllers.UpdateNotificationSetting(c, Services.DB)
	})
	protectedRouter.POST("/notifications/dismiss/:id", "Mark a notification as read", func(c *gin.Context) {
		Controllers.DismissNotification(c, Services.DB)
	})
	protectedRouter.POST("/character/accept/:id", "Accept a character", func(c *gin.Context) {
		Controllers.AcceptCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/deactivate/:id", "Deactivate a character", func(c *gin.Context) {
		Controllers.DeactivateCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/decline/:id", "Decline a pending character", func(c *gin.Context) {
		Controllers.DeclineCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/pending/:id", "Set a character to pending state", func(c *gin.Context) {
		Controllers.PendingCharacter(c, Services.DB)
	})
	protectedRouter.POST("/character/activate/:id", "Activate a character", func(c *gin.Context) {
		Controllers.ActivateCharacter(c, Services.DB)
	})
	protectedRouter.POST("/episode/deactivate/:id", "Deactivate an episode", func(c *gin.Context) {
		Controllers.DeactivateEpisode(c, Services.DB)
	})
	protectedRouter.POST("/episode/:id/warnings-consent", "Record that the user has seen the warnings for an episode", func(c *gin.Context) {
		Controllers.AddEpisodeWarningsConsent(c, Services.DB)
	})
	protectedRouter.POST("/episode/activate/:id", "Activate an episode", func(c *gin.Context) {
		Controllers.ActivateEpisode(c, Services.DB)
	})
	protectedRouter.POST("/wanted-character/deactivate/:id", "Deactivate a wanted character", func(c *gin.Context) {
		Controllers.DeactivateWantedCharacter(c, Services.DB)
	})
	protectedRouter.POST("/wanted-character/activate/:id", "Activate a wanted character", func(c *gin.Context) {
		Controllers.ActivateWantedCharacter(c, Services.DB)
	})
	protectedRouter.POST("/user/avatar", "Upload and save the current user's avatar", func(c *gin.Context) {
		Controllers.UploadUserAvatar(c, Services.DB)
	})
	protectedRouter.POST("/user/do-not-blur", "Set do-not-blur preference for the current user", func(c *gin.Context) {
		Controllers.SetDoNotBlur(c, Services.DB)
	})
	protectedRouter.POST("/user/settings/update", "Update user settings", func(c *gin.Context) {
		Controllers.UpdateSettings(c, Services.DB)
	})
	protectedRouter.POST("/user/absence", "Create an absence record for the current user", func(c *gin.Context) {
		Controllers.CreateAbsence(c, Services.DB)
	})
	protectedRouter.DELETE("/user/absence/:id", "Delete (soft-delete) an absence record owned by the current user", func(c *gin.Context) {
		Controllers.DeleteAbsence(c, Services.DB)
	})
	protectedRouter.POST("/admin/user/:user_id/absence", "Create an absence record for any user (admin)", func(c *gin.Context) {
		Controllers.AdminCreateAbsence(c, Services.DB)
	})
	protectedRouter.POST("/admin/character/immunity", "Add auto-archiving immunity for a character (admin)", func(c *gin.Context) {
		Controllers.AdminAddImmunity(c, Services.DB)
	})
	protectedRouter.POST("/character/immunity/buy", "Buy auto-archiving immunity for a character using currency", func(c *gin.Context) {
		Controllers.BuyAutoArchivingImmunity(c, Services.DB)
	})
	protectedRouter.POST("/user/archive", "Archive the current user's account and deactivate all their characters", func(c *gin.Context) {
		Controllers.ArchiveAccount(c, Services.DB)
	})
	protectedRouter.POST("/admin/user/ban/:id", "Ban a user by ID and deactivate all their characters", func(c *gin.Context) {
		Controllers.BanUser(c, Services.DB)
	})
	protectedRouter.POST("/admin/user/reactivate/:id", "Reactivate an archived user by ID", func(c *gin.Context) {
		Controllers.ReactivateUser(c, Services.DB)
	})
	protectedRouter.GET("/characters/archiving-warnings", "Get active characters approaching auto-archiving threshold", func(c *gin.Context) {
		Controllers.GetArchivingWarnings(c, Services.DB)
	})
	protectedRouter.GET("/admin/user-list", "Get full user list for admin panel", func(c *gin.Context) {
		Controllers.GetAdminUserList(c, Services.DB)
	})
	protectedRouter.GET("/admin/character-list", "Get full character list for admin panel", func(c *gin.Context) {
		Controllers.GetAdminCharacterList(c, Services.DB)
	})
	protectedRouter.GET("/admin/character/:id/protection-history", "Get absences and immunities for a character", func(c *gin.Context) {
		Controllers.GetCharacterProtectionHistory(c, Services.DB)
	})
	protectedRouter.GET("/admin/character/database-field-schema", "Get all unique field machine name + type combinations from character_main", func(c *gin.Context) {
		Controllers.CharacterFieldSchema(c, Services.DB)
	})
	protectedRouter.GET("/admin/character-profile/database-field-schema", "Get all unique field machine name + type combinations from character_profile_main", func(c *gin.Context) {
		Controllers.CharacterProfileFieldSchema(c, Services.DB)
	})
	protectedRouter.GET("/admin/episode/database-field-schema", "Get all unique field machine name + type combinations from episode_main", func(c *gin.Context) {
		Controllers.EpisodeFieldSchema(c, Services.DB)
	})
	protectedRouter.GET("/admin/wanted-character/database-field-schema", "Get all unique field machine name + type combinations from wanted_character_main", func(c *gin.Context) {
		Controllers.WantedCharacterFieldSchema(c, Services.DB)
	})
	protectedRouter.POST("/admin/user/create", "Create a new user account (admin)", func(c *gin.Context) {
		Controllers.CreateUser(c, Services.DB)
	})
	protectedRouter.POST("/admin/user/update/:id", "Update user account (username, avatar) by ID", func(c *gin.Context) {
		Controllers.AdminUpdateUser(c, Services.DB)
	})
	protectedRouter.POST("/mask/create", "Create a new mask", func(c *gin.Context) {
		Controllers.CreateMask(c, Services.DB)
	})
	protectedRouter.POST("/mask/update/:id", "Update mask by ID", func(c *gin.Context) {
		Controllers.UpdateMask(c, Services.DB)
	})
	protectedRouter.POST("/user/save-keys", "Save user's public and private keys", func(c *gin.Context) {
		Controllers.SaveKeys(c, Services.DB)
	})
	protectedRouter.GET("/user/private-key", "Get current user's active private key", func(c *gin.Context) {
		Controllers.GetPrivateKey(c, Services.DB)
	})
	protectedRouter.GET("/user/public-key/:userID", "Get public key by user ID", func(c *gin.Context) {
		Controllers.GetPublicKeyByUserId(c, Services.DB)
	})
	protectedRouter.POST("/direct-chat/create", "Create or find a direct chat with a user", func(c *gin.Context) {
		Controllers.CreateDirectChat(c, Services.DB)
	})
	protectedRouter.POST("/direct-chat/message/create", "Send a direct chat message", func(c *gin.Context) {
		Controllers.CreateDirectChatMessage(c, Services.DB)
	})
	protectedRouter.GET("/direct-chat/:chatID", "Get direct chat details", func(c *gin.Context) {
		Controllers.GetDirectChat(c, Services.DB)
	})
	protectedRouter.GET("/direct-chat/:chatID/messages", "Get messages in a direct chat", func(c *gin.Context) {
		Controllers.GetLastMessages(c, Services.DB)
	})
	protectedRouter.GET("/direct-chat/:chatID/messages/:messageID/before", "Get messages before a given message", func(c *gin.Context) {
		Controllers.GetMessagesBefore(c, Services.DB)
	})
	protectedRouter.GET("/direct-chat/:chatID/messages/:messageID/after", "Get messages after a given message", func(c *gin.Context) {
		Controllers.GetMessagesAfter(c, Services.DB)
	})
	protectedRouter.POST("/direct-chat/:chatID/block", "Block a direct chat", func(c *gin.Context) {
		Controllers.BlockDirectChat(c, Services.DB)
	})
	protectedRouter.POST("/direct-chat/:chatID/unblock", "Unblock a direct chat", func(c *gin.Context) {
		Controllers.UnblockDirectChat(c, Services.DB)
	})
	protectedRouter.GET("/direct-chats", "Get list of current user's direct chats", func(c *gin.Context) {
		Controllers.GetDirectChatList(c, Services.DB)
	})
	protectedRouter.POST("/image/upload", "Upload an image to imgbb", func(c *gin.Context) {
		Controllers.UploadImage(c, Services.DB)
	})
protectedRouter.POST("/category/create", "Create a new category", func(c *gin.Context) {
		Controllers.CreateCategory(c, Services.DB)
	})
	protectedRouter.POST("/category/update/:id", "Update category by ID", func(c *gin.Context) {
		Controllers.UpdateCategory(c, Services.DB)
	})
	protectedRouter.POST("/subforum/create", "Create a new subforum", func(c *gin.Context) {
		Controllers.CreateSubforum(c, Services.DB)
	})
	protectedRouter.POST("/subforum/update/:id", "Update subforum by ID", func(c *gin.Context) {
		Controllers.UpdateSubforum(c, Services.DB)
	})
	protectedRouter.GET("/category/delete/:id", "Delete category by ID", func(c *gin.Context) {
		Controllers.DeleteCategory(c, Services.DB)
	})
	protectedRouter.GET("/subforum/delete/:id", "Delete subforum by ID", func(c *gin.Context) {
		Controllers.DeleteSubforum(c, Services.DB)
	})
	protectedRouter.GET("/user/private-key-check", "Check if user has private keys or private messages", func(c *gin.Context) {
		Controllers.PrivateKeyCheck(c, Services.DB)
	})
	protectedRouter.POST("/user/save-recovery-keys", "Save recovery private keys for the current user", func(c *gin.Context) {
		Controllers.SaveRecoveryKeys(c, Services.DB)
	})
	protectedRouter.GET("/widget-type/list", "Get list of all widget types", func(c *gin.Context) {
		Controllers.GetWidgetTypeList(c, Services.DB)
	})
	protectedRouter.GET("/widget/list", "Get list of all widgets", func(c *gin.Context) {
		Controllers.GetWidgetList(c, Services.DB)
	})
	protectedRouter.GET("/widget-type/:name/config-template", "Get config template for a widget type by name", func(c *gin.Context) {
		Controllers.GetWidgetTypeConfigTemplate(c, Services.DB)
	})
	protectedRouter.POST("/widget/create", "Create a new widget", func(c *gin.Context) {
		Controllers.CreateWidget(c, Services.DB)
	})
	protectedRouter.GET("/widget/:id", "Get widget by ID", func(c *gin.Context) {
		Controllers.GetWidget(c, Services.DB)
	})
	protectedRouter.POST("/widget/:id/update", "Update widget by ID", func(c *gin.Context) {
		Controllers.UpdateWidget(c, Services.DB)
	})
	protectedRouter.GET("/widget/:id/delete", "Delete widget by ID", func(c *gin.Context) {
		Controllers.DeleteWidget(c, Services.DB)
	})
	protectedRouter.GET("/panel/list", "Get list of all panels", func(c *gin.Context) {
		Controllers.GetPanelList(c, Services.DB)
	})
	protectedRouter.GET("/panel/:key", "Get panel by key", func(c *gin.Context) {
		Controllers.GetPanelByName(c, Services.DB)
	})

	protectedRouter.POST("/panel/:key/update", "Update panel by key", func(c *gin.Context) {
		Controllers.UpdatePanelByName(c, Services.DB)
	})
	protectedRouter.POST("/panel/preview", "Preview rendered panel content", func(c *gin.Context) {
		Controllers.PanelPreview(c, Services.DB)
	})
	protectedRouter.POST("/static-file/upload", "Upload a static file", func(c *gin.Context) {
		Controllers.UploadFile(c, Services.DB)
	})
	publicRouter.GET("/reaction/list", "Get list of all reactions", func(c *gin.Context) {
		Controllers.GetReactionList(c, Services.DB)
	})
	protectedRouter.POST("/reaction/create", "Upload and create a new reaction", func(c *gin.Context) {
		Controllers.CreateReaction(c, Services.DB)
	})
	protectedRouter.POST("/reaction/deactivate/:id", "Deactivate a reaction by ID", func(c *gin.Context) {
		Controllers.DeactivateReaction(c, Services.DB)
	})
	protectedRouter.POST("/reaction/activate/:id", "Activate a reaction by ID", func(c *gin.Context) {
		Controllers.ActivateReaction(c, Services.DB)
	})
	publicRouter.GET("/reaction/list/active", "Get list of active reactions", func(c *gin.Context) {
		Controllers.GetActiveReactionList(c, Services.DB)
	})
	optionalAuthRouter.POST("/post-reaction/create", "React to a post", func(c *gin.Context) {
		Controllers.ReactToPost(c, Services.DB)
	})
	protectedRouter.GET("/static-file/list/:file_type", "Get last 3 static files by type", func(c *gin.Context) {
		Controllers.GetStaticFileList(c, Services.DB)
	})
	protectedRouter.POST("/static-file/revert", "Revert to a specific static file version", func(c *gin.Context) {
		Controllers.AdminRevertStaticFile(c, Services.DB)
	})
	protectedRouter.POST("/design-variation/create", "Create a new design variation", func(c *gin.Context) {
		Controllers.CreateDesignVariation(c, Services.DB)
	})
	protectedRouter.GET("/design-variation/delete/:id", "Delete design variation by ID", func(c *gin.Context) {
		Controllers.DeleteDesignVariation(c, Services.DB)
	})
	protectedRouter.GET("/design-variation/list", "Get list of all design variations", func(c *gin.Context) {
		Controllers.GetDesignVariationList(c, Services.DB)
	})
	protectedRouter.POST("/design-variation/update/:id", "Update design variation by ID", func(c *gin.Context) {
		Controllers.UpdateDesignVariation(c, Services.DB)
	})
	protectedRouter.GET("/admin/design-draft/list", "Get list of all design drafts without CSS content", func(c *gin.Context) {
		Controllers.GetDesignDraftList(c, Services.DB)
	})
	protectedRouter.GET("/admin/design-draft/get/:id", "Get a design draft by ID with full CSS content", func(c *gin.Context) {
		Controllers.GetDesignDraft(c, Services.DB)
	})
	protectedRouter.POST("/admin/design-draft/create", "Create a new design draft from current CSS files", func(c *gin.Context) {
		Controllers.CreateDesignDraft(c, Services.DB)
	})
	protectedRouter.POST("/admin/design-draft/update/:id", "Update a design draft by ID", func(c *gin.Context) {
		Controllers.UpdateDesignDraft(c, Services.DB)
	})
	protectedRouter.DELETE("/admin/design-draft/delete/:id", "Delete a design draft by ID", func(c *gin.Context) {
		Controllers.DeleteDesignDraft(c, Services.DB)
	})
	protectedRouter.POST("/admin/design-draft/publish/:id", "Publish a design draft to the live CSS files", func(c *gin.Context) {
		Controllers.PublishDesignDraft(c, Services.DB)
	})
	protectedRouter.POST("/admin/additional-navlink/create", "Create a new additional navlink", func(c *gin.Context) {
		Controllers.CreateAdditionalNavlink(c, Services.DB)
	})
	protectedRouter.POST("/admin/additional-navlink/update/:id", "Update additional navlink by ID", func(c *gin.Context) {
		Controllers.UpdateAdditionalNavlink(c, Services.DB)
	})
	protectedRouter.GET("/admin/additional-navlink/list", "Get admin list of all additional navlinks", func(c *gin.Context) {
		Controllers.GetAdditionalNavlinkList(c, Services.DB)
	})
	protectedRouter.GET("/admin/additional-navlink/delete/:id", "Delete additional navlink by ID", func(c *gin.Context) {
		Controllers.DeleteAdditionalNavlink(c, Services.DB)
	})
	protectedRouter.GET("/admin/smile/list", "Get flat list of all smiles", func(c *gin.Context) {
		Controllers.GetSmileList(c, Services.DB)
	})
	protectedRouter.POST("/admin/smile/upload", "Upload a smile image", func(c *gin.Context) {
		Controllers.UploadSmile(c, Services.DB)
	})
	protectedRouter.GET("/admin/smile/delete/:id", "Delete smile by ID", func(c *gin.Context) {
		Controllers.DeleteSmile(c, Services.DB)
	})
	protectedRouter.POST("/admin/smile/update-category/:id", "Update smile's category by ID", func(c *gin.Context) {
		Controllers.UpdateCategoryId(c, Services.DB)
	})
	protectedRouter.GET("/admin/smile-category/list", "Get list of all smile categories", func(c *gin.Context) {
		Controllers.GetSmileCategoryList(c, Services.DB)
	})
	protectedRouter.POST("/admin/smile-category/create", "Create a new smile category", func(c *gin.Context) {
		Controllers.CreateSmileCategory(c, Services.DB)
	})
	protectedRouter.POST("/admin/smile-category/update/:id", "Update smile category by ID", func(c *gin.Context) {
		Controllers.UpdateSmileCategory(c, Services.DB)
	})
	protectedRouter.GET("/admin/smile-category/delete/:id", "Delete smile category by ID", func(c *gin.Context) {
		Controllers.DeleteSmileCategory(c, Services.DB)
	})
	protectedRouter.GET("/admin/role/list", "Get list of all roles", func(c *gin.Context) {
		Controllers.GetRoleList(c, Services.DB)
	})
	protectedRouter.GET("/admin/home", "Get admin home categories (all, including empty)", func(c *gin.Context) {
		Controllers.GetAdminHomeCategories(c, Services.DB)
	})
	protectedRouter.GET("/admin/health", "Get RAM and CPU health data", func(c *gin.Context) {
		Controllers.GetHealthData(c)
	})
	protectedRouter.GET("/admin/frontend-templates/components", "List customizable frontend components", func(c *gin.Context) {
		Controllers.GetFrontendComponents(c, Services.DB)
	})
	protectedRouter.GET("/admin/frontend-templates/components/*name", "Get the custom template for a frontend component", func(c *gin.Context) {
		Controllers.GetFrontendComponentTemplate(c, Services.DB)
	})
	protectedRouter.GET("/admin/frontend-templates/components-default/*name", "Get the default template for a frontend component", func(c *gin.Context) {
		Controllers.GetFrontendComponentDefaultTemplate(c, Services.DB)
	})
	protectedRouter.POST("/admin/frontend-templates/component/update", "Commit an update to a custom frontend component template", func(c *gin.Context) {
		Controllers.UpdateFrontendComponentTemplate(c, Services.DB)
	})
	protectedRouter.POST("/admin/frontend-templates/env/update", "Update and commit the frontend environment file", func(c *gin.Context) {
		Controllers.UpdateFrontendEnv(c, Services.DB)
	})
	protectedRouter.POST("/admin/github/commit", "Commit files to the frontend GitHub repository", func(c *gin.Context) {
		Controllers.CommitFrontendTemplates(c, Services.DB)
	})
	protectedRouter.GET("/admin/sonic/cursors", "Get Sonic ingest cursors for all buckets", func(c *gin.Context) {
		Controllers.GetSonicCursors(c, Services.DB)
	})
	protectedRouter.POST("/admin/sonic/catchup/:bucket", "Catch up Sonic ingestion for a specific bucket", func(c *gin.Context) {
		Controllers.CatchUpSonicBucket(c, Services.DB)
	})
	protectedRouter.GET("/admin/qdrant/cursors", "Get Qdrant ingest cursors for all buckets", func(c *gin.Context) {
		Controllers.GetQdrantCursors(c, Services.DB)
	})
	protectedRouter.GET("/admin/qdrant/status", "Get Qdrant vector counts per collection", func(c *gin.Context) {
		Controllers.GetQdrantCollectionStatus(c)
	})
	protectedRouter.POST("/admin/qdrant/catchup/:bucket", "Re-embed and upsert all content for a Qdrant bucket", func(c *gin.Context) {
		Controllers.QdrantCatchUpBucket(c, Services.DB)
	})
	protectedRouter.GET("/admin/qdrant/subforum-matrix", "Get vector search subforum×bucket matrix", func(c *gin.Context) {
		Controllers.GetVectorSearchMatrix(c, Services.DB)
	})
	protectedRouter.POST("/admin/qdrant/subforum-matrix/update", "Replace all vector search subforum+bucket entries", func(c *gin.Context) {
		Controllers.UpdateVectorSearchMatrix(c, Services.DB)
	})
	protectedRouter.GET("/admin/user/roles/:id", "Get user roles", func(c *gin.Context) {
		Controllers.GetUserRoles(c, Services.DB)
	})
	protectedRouter.POST("/admin/user/roles/update", "Update user roles", func(c *gin.Context) {
		Controllers.UpdateUserRoles(c, Services.DB)
	})

	// AI Chat routes
	protectedRouter.POST("/ai-chat/message", "Send a message to the AI", func(c *gin.Context) {
		MCP.SendMessage(c, Services.DB)
	})
	protectedRouter.GET("/ai-chat/history", "Get AI chat history for the current user", func(c *gin.Context) {
		MCP.GetAIChatHistory(c, Services.DB)
	})
	protectedRouter.GET("/ai-chat/models", "Get available AI models for the configured provider", func(c *gin.Context) {
		MCP.GetAvailableModels(c, Services.DB)
	})
	protectedRouter.POST("/ai-chat/clear", "Clear AI chat context for the current user", func(c *gin.Context) {
		MCP.ClearAIContext(c, Services.DB)
	})

	// Fraction settings routes
	publicRouter.GET("/faction/:faction_id/free-format-date", "Get free format date config for a faction", func(c *gin.Context) {
		Controllers.GetFactionFreeFormatDate(c, Services.DB)
	})
	publicRouter.POST("/factions/free-format-date", "Get free format date templates for factions by character IDs", func(c *gin.Context) {
		Controllers.GetFactionFreeFormatDateByCharacters(c, Services.DB)
	})
	publicRouter.GET("/faction-settings/list", "Get list of all faction settings ordered by level", func(c *gin.Context) {
		Controllers.GetFactionSettings(c, Services.DB)
	})
	protectedRouter.POST("/admin/faction-setting/create", "Create a new faction setting", func(c *gin.Context) {
		Controllers.CreateFactionSetting(c, Services.DB)
	})
	protectedRouter.POST("/admin/faction-setting/update/:id", "Update faction setting by ID", func(c *gin.Context) {
		Controllers.UpdateFactionSetting(c, Services.DB)
	})

	protectedRouter.GET("/admin/free-format-date-settings", "List all free format date settings", func(c *gin.Context) {
		Controllers.ListFreeFormatDateSettings(c, Services.DB)
	})
	protectedRouter.GET("/admin/free-format-date-settings/options", "List free format date setting ids and names", func(c *gin.Context) {
		Controllers.ListFreeFormatDateSettingOptions(c, Services.DB)
	})
	protectedRouter.GET("/admin/free-format-date-settings/:id", "Get a single free format date setting", func(c *gin.Context) {
		Controllers.GetFreeFormatDateSetting(c, Services.DB)
	})

	protectedRouter.POST("/admin/free-format-date-settings", "Create a free format date setting", func(c *gin.Context) {
		Controllers.CreateFreeFormatDateSetting(c, Services.DB)
	})
	protectedRouter.POST("/admin/free-format-date-settings/:id", "Update a free format date setting", func(c *gin.Context) {
		Controllers.UpdateFreeFormatDateSetting(c, Services.DB)
	})
	protectedRouter.DELETE("/admin/free-format-date-settings/:id", "Delete a free format date setting", func(c *gin.Context) {
		Controllers.DeleteFreeFormatDateSetting(c, Services.DB)
	})
	protectedRouter.GET("/admin/faction-setting/delete/:id", "Delete faction setting by ID", func(c *gin.Context) {
		Controllers.DeleteFactionSetting(c, Services.DB)
	})

	// External app public endpoints
	publicRouter.POST("/external-app/post", "Create a post as an external app (authenticated via X-Api-Key header)", func(c *gin.Context) {
		Controllers.ExternalAppPost(c, Services.DB)
	})
	optionalAuthRouter.GET("/puzzles", "Get list of puzzles", func(c *gin.Context) {
		Controllers.GetPuzzles(c, Services.DB)
	})
	optionalAuthRouter.GET("/puzzle/:id", "Get a single puzzle by ID", func(c *gin.Context) {
		Controllers.GetPuzzle(c, Services.DB)
	})
	optionalAuthRouter.GET("/user/:user_id/puzzle-achievements", "Get puzzle achievements for a user", func(c *gin.Context) {
		Controllers.GetUserPuzzleAchievements(c, Services.DB)
	})
	protectedRouter.POST("/puzzle/:id/achievement", "Save a puzzle achievement", func(c *gin.Context) {
		Controllers.SavePuzzleAchievement(c, Services.DB)
	})
	protectedRouter.DELETE("/puzzle/achievement/:id", "Delete own puzzle achievement", func(c *gin.Context) {
		Controllers.DeletePuzzleAchievement(c, Services.DB)
	})
	protectedRouter.GET("/admin/puzzle/list", "Get all puzzles including inactive (admin)", func(c *gin.Context) {
		Controllers.AdminGetPuzzles(c, Services.DB)
	})
	protectedRouter.GET("/admin/puzzle/:id", "Get a single puzzle by ID (admin)", func(c *gin.Context) {
		Controllers.AdminGetPuzzle(c, Services.DB)
	})
	protectedRouter.POST("/admin/puzzle/create", "Create a new puzzle (admin)", func(c *gin.Context) {
		Controllers.AdminCreatePuzzle(c, Services.DB)
	})
	protectedRouter.POST("/admin/puzzle/update/:id", "Update a puzzle (admin)", func(c *gin.Context) {
		Controllers.AdminUpdatePuzzle(c, Services.DB)
	})

	publicRouter.GET("/external-app/active-topics", "Get active topics for an external app (authenticated via X-Api-Key header)", func(c *gin.Context) {
		Controllers.ExternalAppGetActiveTopics(c, Services.DB)
	})
	publicRouter.GET("/external-app/topic-first-post", "Get the first post content of a topic (authenticated via X-Api-Key header)", func(c *gin.Context) {
		Controllers.ExternalAppGetTopicFirstPost(c, Services.DB)
	})
	publicRouter.GET("/external-app/get-post/:id", "Get a post by ID (authenticated via X-Api-Key header)", func(c *gin.Context) {
		Controllers.ExternalAppGetPost(c, Services.DB)
	})
	publicRouter.POST("/external-app/update-post/:id", "Update a post by ID (authenticated via X-Api-Key header)", func(c *gin.Context) {
		Controllers.ExternalAppUpdatePost(c, Services.DB)
	})

	// External apps routes
	protectedRouter.GET("/admin/external-app/list", "Get list of all external apps", func(c *gin.Context) {
		Controllers.GetExternalAppList(c, Services.DB)
	})
	protectedRouter.POST("/admin/external-app/create", "Create a new external app with a generated API key", func(c *gin.Context) {
		Controllers.CreateExternalApp(c, Services.DB)
	})
	protectedRouter.POST("/admin/external-app/update/:id", "Update external app name or status by ID", func(c *gin.Context) {
		Controllers.UpdateExternalApp(c, Services.DB)
	})
	protectedRouter.GET("/admin/external-app/delete/:id", "Delete external app by ID", func(c *gin.Context) {
		Controllers.DeleteExternalApp(c, Services.DB)
	})
	protectedRouter.GET("/admin/external-app/:id/permissions", "Get permissions for an external app", func(c *gin.Context) {
		Controllers.GetExternalAppPermissions(c, Services.DB)
	})
	protectedRouter.POST("/admin/external-app/:id/permission/create", "Add a permission to an external app", func(c *gin.Context) {
		Controllers.AddExternalAppPermission(c, Services.DB)
	})
	protectedRouter.POST("/admin/external-app/:id/permission/delete", "Remove a permission from an external app", func(c *gin.Context) {
		Controllers.DeleteExternalAppPermission(c, Services.DB)
	})

	// Standard warnings routes
	protectedRouter.GET("/standard-warnings", "Get list of standard warnings", func(c *gin.Context) {
		Controllers.GetStandardWarnings(c, Services.DB)
	})
	protectedRouter.GET("/admin/standard-warning/list", "Get list of all standard warnings (admin)", func(c *gin.Context) {
		Controllers.GetStandardWarnings(c, Services.DB)
	})
	protectedRouter.POST("/admin/standard-warning/create", "Create a new standard warning", func(c *gin.Context) {
		Controllers.CreateStandardWarning(c, Services.DB)
	})
	protectedRouter.POST("/admin/standard-warning/update/:id/:locale", "Update standard warning by ID and locale", func(c *gin.Context) {
		Controllers.UpdateStandardWarning(c, Services.DB)
	})
	protectedRouter.GET("/admin/standard-warning/delete/:id/:locale", "Delete standard warning by ID and locale", func(c *gin.Context) {
		Controllers.DeleteStandardWarning(c, Services.DB)
	})

	// User data migration routes
	protectedRouter.GET("/user-data-migration/list", "Get list of all data migration processings for the current user", func(c *gin.Context) {
		Controllers.GetUserDataProcessingList(c, Services.DB)
	})
	protectedRouter.GET("/user-data-migration/processing/:id", "Get a single user data processing record by ID", func(c *gin.Context) {
		Controllers.GetUserDataProcessing(c, Services.DB)
	})
	protectedRouter.POST("/user-data-migration/create-processing", "Create a new user data processing record in pending status", func(c *gin.Context) {
		Controllers.CreateUserDataProcessing(c, Services.DB)
	})
	protectedRouter.POST("/user-data-migration/process-mybb-topic", "Parse a MyBB topic JSON export and save posts to user_data_migration", func(c *gin.Context) {
		Controllers.ProcessMybbTopicJson(c, Services.DB)
	})
	protectedRouter.POST("/user-data-migration/publish", "Publish processed migration posts to a topic in original order", func(c *gin.Context) {
		Controllers.UserDataProcessingPublish(c, Services.DB)
	})
	protectedRouter.POST("/user-data-migration/update-character-map", "Set character IDs for original user IDs in a processing record", func(c *gin.Context) {
		Controllers.UpdateUserCharacterMap(c, Services.DB)
	})

	// WebSocket route with special authentication
	wsGroup := r.Group("/")
	wsGroup.Use(Middlewares.WebSocketAuthMiddleware())
	wsRouter := Router.NewCustomRouter(wsGroup)
	wsRouter.GET("/ws", "WebSocket connection endpoint", func(c *gin.Context) {
		Controllers.HandleWebSocket(c, Services.DB)
	})

	r.Run() // listen and serve on 0.0.0.0:8080
}
