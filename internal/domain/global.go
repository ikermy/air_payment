package domain

var (
	UsersDB = make(chan struct{}) // Канал уведомления о завершении операций пользователями ДБ
	Exit    = make(chan struct{}) // Канал завершения работы приложения
)
