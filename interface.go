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

	// AddRoute - добавить обработчик
	AddRoute(subroute string, route string, handler http.HandlerFunc, methods ...string)
	// AddMiddleware - добавить цепочку обработчиков на промежуточном уровне
	AddMiddleware(subroute string, mwf ...MiddlewareFunc)

	// StartSession - запомнить новую сессию после логина. В ответах пользователю будет добавлен куки
	StartSession(w http.ResponseWriter, r *http.Request, userID string, sessionAge int, cookieName string, cookieKey string) error
	// CheckSession - проверить залогинен ли пользователь
	CheckSession(r *http.Request, cookieName string, cookieKey string) (userID string, err error)
	// CloseSession - закрыть сессию
	CloseSession(w http.ResponseWriter, r *http.Request, cookieName string, cookieKey string)
}
