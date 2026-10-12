package subs

const (
	StubExpired         = "⛔ Подписка истекла — продли в боте"
	StubTrafficExceeded = "⛔ Закончился трафик — продли в боте"
	StubDeviceLimit     = "⛔ Лимит устройств — сбрось устройства в боте"
	StubHWIDRequired    = "⛔ Клиент не поддерживается — нужна авторизация по HWID"
	StubTrialUsed       = "⛔ Пробный период уже использован на этом устройстве — купи тариф в боте"
	StubDenied          = "⛔ Доступ закрыт — напиши в поддержку"
	StubNoServers       = "⚠️ Нет доступных серверов — напиши в поддержку"
	StubSingBox         = "⚠️ sing-box пока не поддерживается — поставь Happ или v2RayTun"
)

var statusStubs = map[string]string{
	StatusExpired:         StubExpired,
	StatusTrafficExceeded: StubTrafficExceeded,
	StatusDeviceLimit:     StubDeviceLimit,
	StatusHWIDRequired:    StubHWIDRequired,
	StatusTrialUsed:       StubTrialUsed,
}

func content(view *View, format Format) ([]Connection, string) {
	if view.Status != StatusOK {
		if text, ok := statusStubs[view.Status]; ok {
			return nil, text
		}
		return nil, StubDenied
	}
	if format == FormatSingBox {
		return nil, StubSingBox
	}
	conns := make([]Connection, 0, len(view.Connections))
	for _, c := range view.Connections {
		if c.Transport == "tcp" && c.Security == "reality" {
			conns = append(conns, c)
		}
	}
	if len(conns) == 0 {
		return nil, StubNoServers
	}
	return conns, ""
}
