package schema

var notificationFunctions = baseNotificationFunctions + reviewNotificationFunctions

var notificationSchema = notificationFunctions + notificationTriggers + communityNotificationsV8
