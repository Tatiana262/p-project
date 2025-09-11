package constants

// Имена очередей
const (
	QueueLinkTasks          = "link_parsing_tasks"
	QueueProcessedProperties = "processed_properties"
)

// Ключи маршрутизации
const (
	RoutingKeyLinkTasks          = "kufar.links.tasks"
	RoutingKeyProcessedProperties = "db.properties.save"
)