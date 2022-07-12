package httprouter

import "net/http"

type CompressionType int

const (
	CompressionNo = CompressionType(iota)
	CompressionGzip
	CompressionDeflate
)

type MiddlewareFunc func(next http.Handler) http.Handler

// Router - интерфейс http роутера
// Создан для исключения зависимости обработчиков запросов от используемого роутера
type Router interface {
	// RespondData - ответ на запрос
	// data содержит []byte или указатель на объект. Во втором случае этот объект преобразуется в JSON */
	RespondData(w http.ResponseWriter, code int, contentType string, data interface{})
	// RespondCompressed - ответ на запрос
	// data содержит []byte или указатель на объект. Во втором случае этот объект преобразуется в JSON.
	// Дополнительно проверяет заголовок запроса на "Accept-Encoding" и решает сжимать ли ответ на самом деле,
	// т.е. в итоге ответ может быть и без сжатия
	RespondCompressed(w http.ResponseWriter, r *http.Request, code int, compType CompressionType, contentType string, data interface{})
	// RespondError - возврат ошибки
	RespondError(w http.ResponseWriter, code int, err error)

	// AddRoute - добавить обработчик. Поддерживает пути вида:
	// /products
	// /products/{key}
	// /articles/{category}/{id:[0-9]+}").
	AddRoute(subroute string, route string, handler http.HandlerFunc, methods ...string)
	// AddMiddleware - добавить цепочку обработчиков на промежуточном уровне
	AddMiddleware(subroute string, mwf ...MiddlewareFunc)

	// Возвращает переменные запроса. Переменные - это часть URL для которых были заданы маски в методе AddRoute
	// Значения ключа - это имена переменных. Например для /products/{key} - имя ключа будет key
	GetVars(r *http.Request) map[string]string
	// Возвращает переменную запроса по ее ключу. Переменные - это часть URL для которых мали заданы маски в методе AddRoute
	GetVar(r *http.Request, key string) string

	// Возвращает параметры запроса. Параметры передаются после знака вопроса
	GetParams(r *http.Request) map[string][]string
	// Возвращает параметр запроса по его ключу. Параметры передаются после знака вопроса
	GetParam(r *http.Request, key string) string

	// StartSession - запомнить новую сессию после логина. В ответах пользователю будет добавлен куки
	StartSession(w http.ResponseWriter, r *http.Request, userID string, sessionAge int, cookieName, cookieKey string,
		secure, httpOnly bool) error
	// CheckSession - проверить залогинен ли пользователь
	CheckSession(r *http.Request, cookieName string, cookieKey string) (userID string, err error)
	// CloseSession - закрыть сессию
	CloseSession(w http.ResponseWriter, r *http.Request, cookieName string, cookieKey string)
}
