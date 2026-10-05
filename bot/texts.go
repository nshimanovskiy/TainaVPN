package main

// Bot texts. HTML formatting (Telegram parse_mode=HTML).
var texts = map[string]map[string]string{
	"ru": {
		"welcome": "👋 Это бот <b>Tainavpn</b>.\n\n" +
			"Здесь можно получить личную ссылку-подписку и скачать приложение.\n" +
			"В приложении будут прокси нескольких стран — выбирайте любую.",
		"btnLink":    "🔗 Моя ссылка-подписка",
		"btnApp":     "📥 Скачать приложение",
		"btnHelp":    "❓ Как подключиться",
		"btnNew":     "🔄 Выпустить новую ссылку",
		"btnNewYes":  "Да, выпустить новую",
		"btnCancel":  "Отмена",
		"btnPartner": "🛒 Купить свои прокси",
		"cmdLink":    "Моя ссылка-подписка",
		"cmdApp":     "Скачать приложение",
		"cmdHelp":    "Как подключиться",
		"link":       "🔗 Ваша ссылка-подписка:\n\n<code>%s</code>\n\n(нажмите на ссылку, чтобы скопировать)",
		"linkNew":    "✅ Выпущена новая ссылка, старая больше не работает:\n\n<code>%s</code>\n\n(нажмите на ссылку, чтобы скопировать)",
		"countries":  "Доступные страны:",
		"howTo": "<b>Как подключиться:</b>\n" +
			"1. Скачайте и установите приложение Tainavpn (кнопка ниже).\n" +
			"2. В приложении нажмите <b>+ Добавить</b> → <b>По ссылке</b> и вставьте ссылку.\n" +
			"3. Выберите страну и нажмите большую кнопку.",
		"help": "<b>Как подключиться:</b>\n" +
			"1. Нажмите «📥 Скачать приложение» и установите Tainavpn для своей системы.\n" +
			"   • Windows — просто запустите файл, разрешите права администратора.\n" +
			"   • Android — откройте файл и разрешите установку из этого источника.\n" +
			"   • Ubuntu — откройте .deb двойным кликом или: <code>sudo apt install ./файл.deb</code>\n" +
			"2. Нажмите «🔗 Моя ссылка-подписка» и скопируйте ссылку.\n" +
			"3. В приложении: <b>+ Добавить</b> → <b>По ссылке</b> → вставьте ссылку.\n" +
			"4. Выберите страну и нажмите большую кнопку подключения.\n\n" +
			"Ссылка личная — не передавайте её другим. Если она попала к кому-то, выпустите новую.",
		"newConfirm":      "Выпустить новую ссылку? Старая перестанет работать, и её нужно будет заменить в приложении.",
		"chooseApp":       "Выберите систему:",
		"caption_windows": "Tainavpn для Windows 10/11. Запустите файл — установка не нужна.",
		"caption_android": "Tainavpn для Android 7+. Откройте файл и разрешите установку.",
		"caption_ubuntu":  "Tainavpn для Ubuntu 22.04/24.04. Установка: двойной клик или <code>sudo apt install ./файл.deb</code>",
		"noFile":          "Файл пока недоступен, попробуйте позже.",
		"uploadFailed":    "Не получилось отправить файл, попробуйте позже.",
		"serverDown":      "Сервер временно недоступен, попробуйте позже.",
		"disabled":        "⛔️ Ваш доступ отключён администратором.",
		"denied":          "⛔️ Доступ закрыт. Ваш Telegram ID: <code>%d</code> — сообщите его администратору.",
	},
	"en": {
		"welcome": "👋 This is the <b>Tainavpn</b> bot.\n\n" +
			"Here you can get your personal subscription link and download the app.\n" +
			"The app will have proxies in several countries — pick any.",
		"btnLink":    "🔗 My subscription link",
		"btnApp":     "📥 Download the app",
		"btnHelp":    "❓ How to connect",
		"btnNew":     "🔄 Issue a new link",
		"btnNewYes":  "Yes, issue a new one",
		"btnCancel":  "Cancel",
		"btnPartner": "🛒 Buy your own proxies",
		"cmdLink":    "My subscription link",
		"cmdApp":     "Download the app",
		"cmdHelp":    "How to connect",
		"link":       "🔗 Your subscription link:\n\n<code>%s</code>\n\n(tap the link to copy it)",
		"linkNew":    "✅ A new link has been issued, the old one no longer works:\n\n<code>%s</code>\n\n(tap the link to copy it)",
		"countries":  "Available countries:",
		"howTo": "<b>How to connect:</b>\n" +
			"1. Download and install the Tainavpn app (button below).\n" +
			"2. In the app tap <b>+ Add</b> → <b>By link</b> and paste the link.\n" +
			"3. Choose a country and press the big button.",
		"help": "<b>How to connect:</b>\n" +
			"1. Tap “📥 Download the app” and install Tainavpn for your system.\n" +
			"   • Windows — just run the file and allow administrator rights.\n" +
			"   • Android — open the file and allow installing from this source.\n" +
			"   • Ubuntu — open the .deb with a double click or: <code>sudo apt install ./file.deb</code>\n" +
			"2. Tap “🔗 My subscription link” and copy the link.\n" +
			"3. In the app: <b>+ Add</b> → <b>By link</b> → paste the link.\n" +
			"4. Choose a country and press the big connect button.\n\n" +
			"The link is personal — don't share it. If someone got it, issue a new one.",
		"newConfirm":      "Issue a new link? The old one will stop working and you'll need to replace it in the app.",
		"chooseApp":       "Choose your system:",
		"caption_windows": "Tainavpn for Windows 10/11. Run the file — no installation needed.",
		"caption_android": "Tainavpn for Android 7+. Open the file and allow the installation.",
		"caption_ubuntu":  "Tainavpn for Ubuntu 22.04/24.04. Install: double click or <code>sudo apt install ./file.deb</code>",
		"noFile":          "The file is not available yet, try again later.",
		"uploadFailed":    "Could not send the file, try again later.",
		"serverDown":      "The server is temporarily unavailable, try again later.",
		"disabled":        "⛔️ Your access has been disabled by the administrator.",
		"denied":          "⛔️ Access is closed. Your Telegram ID: <code>%d</code> — send it to the administrator.",
	},
}
