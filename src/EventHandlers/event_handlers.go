package EventHandlers

import (
	"database/sql"
)

func RegisterEventHandlers(db *sql.DB) {
	RegisterTopicEventHandlers()
	RegisterPostEventHandlers()
	RegisterNotificationEventHandlers()
	RegisterCharacterEventHandlers()
	RegisterEpisodeEventHandlers()
	RegisterUserEventHandlers()
	RegisterDirectChatEventHandlers()
	RegisterWantedCharacterEventHandlers()
	RegisterStaticFileEventHandlers()
	RegisterReactionEventHandlers()
	RegisterSonicEventHandlers()
}
