package notifiers

// Compile-time checks that all notifier types implement the Notifier interface.
var (
	_ Notifier = (*Telegram)(nil)
	_ Notifier = (*Discord)(nil)
	_ Notifier = (*Ntfy)(nil)
	_ Notifier = (*Webhook)(nil)
)
