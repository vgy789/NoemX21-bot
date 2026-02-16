---
name: Booking and Library Spec Alignment
overview: "Привести код бронирования и библиотеки в соответствие со спецификациями booking.yaml и library.yaml: исправить имена действий и переменных, добавить недостающие действия и заполнение контекста для экранов CONFIRM_FAST и поиска."
todos: []
isProject: false
---

# План приведения кода к новым интерфейсам (booking / library)

## Почему не работали поиск и бронирование

- **Booking:** в спеке первое действие — `get_dashboard_data`, в коде зарегистрировано только `get_booking_data` → движок выдаёт "action not found" и поток не стартует.
- **Library:** в спеке старт — `AUTO_SYNC_USER_STATS` с действием `get_user_summary`, в коде — `get_library_stats` и алиас `LIBRARY_MENU` → `library.yaml/AUTO_SYNC_LIB` (такого состояния в YAML нет). Поиск вызывает `search_books` с параметрами `query`, `page`, `limit`, `only_available`; в коде есть только `get_books` с `search`, `offset` и другими именами.

Ниже — согласование кода со спеками без изменения контракта YAML (имена действий, состояний, переменных интерфейса).

---

## 1. Booking

### 1.1 Имя и контракт действия дашборда

- **Файл:** [internal/fsm/actions/booking/booking.go](internal/fsm/actions/booking/booking.go)
- Спека: `AUTO_SYNC_BOOKING` вызывает `**get_dashboard_data**` с `payload.campus_id`.
- Сейчас: зарегистрировано только `get_booking_data`.
- **Сделать:** зарегистрировать действие с именем `**get_dashboard_data**` (можно вызвать из той же функции, что и текущий `get_booking_data`, или зарегистрировать алиас в registry). Важно: в спеке используется именно `get_dashboard_data`.

### 1.2 Переменные дашборда (BOOKING_DASHBOARD)

Спек ожидает в интерфейсе:

- `my_campus`, `free_rooms_count`, `busy_rooms_count`, `hot_slots_list`, `dashboard_visualization`
- Кнопки слотов строятся из переменных (см. п. 1.4).

Текущий код отдаёт `free_slots_list`, `current_date`, `selected_date`, `slot_id_1`…`slot_id_12`, `slot_label_1`…`slot_label_12`.

- **Сделать:**
  - Возвращать из действия дашборда: `my_campus` (название кампуса), `free_rooms_count`, `busy_rooms_count`, `hot_slots_list` (текст списка слотов, можно переиспользовать текущий список), при необходимости `dashboard_visualization` (пустая строка или заглушка).
  - Дополнительно возвращать `**slot_room_name_1`…`slot_room_name_12**` и `**slot_time_1`…`slot_time_12**` (уже есть в структуре слотов), чтобы при переходе в CONFIRM_FAST можно было подставить `room_name` и `start_time` из контекста.

### 1.3 Динамические кнопки слотов на дашборде

В [booking.yaml](docs/specs/flows/booking.yaml) в `BOOKING_DASHBOARD` в `buttons` перечислены только статичные кнопки (open_calendar, my_bookings, refresh). Переход по нажатию слота задан через `transitions` с условием `id.startswith('slot_')`, но движок определяет переход только по совпадению **кнопки** с `input` ([internal/fsm/engine.go](internal/fsm/engine.go) — `findNextState` по `Interface.Buttons`), а не по переходам с `trigger`/`condition`.

- **Сделать:** в `docs/specs/flows/booking.yaml` в состоянии `BOOKING_DASHBOARD` добавить в `buttons` до 12 динамических кнопок слотов, например:
  - `id: "{slot_id_1}"`, `label: "{slot_label_1}"`, `next_state: CONFIRM_FAST`, при необходимости `row: 1`
  - и так для `slot_id_2`…`slot_id_12` (и соответствующих `slot_label_*`), чтобы отображались и обрабатывались нажатия по слоту.

### 1.4 Контекст для CONFIRM_FAST

В CONFIRM_FAST в интерфейсе используются `{room_name}` и `{start_time}`. После нажатия слота в контексте есть только `last_input` (например `slot_5_10:00`).

- **Сделать:** при переходе в CONFIRM_FAST заполнять `room_id`, `room_name`, `start_time` (и при необходимости `time` для create_booking). Варианты:
  - **Вариант A:** добавить в [booking.yaml](docs/specs/flows/booking.yaml) у состояния CONFIRM_FAST блок `**on_enter**` с одним вызовом действия `**resolve_slot_from_last_input**`, которое по `last_input` (формат `slot_<room_id>_<time>`) и по уже сохранённым в контексте `slot_room_name_*`, `slot_time_*` (или парсингом из `slot_id_*`) выставляет в контекст `room_id`, `room_name`, `start_time` (и `time`). Зарегистрировать это действие в [internal/fsm/actions/booking/booking.go](internal/fsm/actions/booking/booking.go).
  - **Вариант B:** в движке при рендере состояния CONFIRM_FAST, если `last_input` начинается с `slot_`, по нему и по `slot_*` из контекста вычислить и подставить `room_id`, `room_name`, `start_time` в контекст перед рендером (без изменения YAML).

Рекомендуется вариант A (явное действие и on_enter), чтобы не размазывать логику бронирования по движку.

### 1.5 create_booking

Спек: payload только `room_id`, `time`, `duration`. Текущий код ожидает также `campus_id` и `date`.

- **Сделать:**
  - Брать `campus_id` и при необходимости `date` из контекста (например из результата `get_dashboard_data` или из текущего контекста пользователя); если `date` нет — считать «сегодня».
  - При успехе возвращать в контекст переменные для экрана COMPLETED: `**room_name**`, `**time_interval**` (например `"10:00–10:30"`), чтобы не зависеть от того, что осталось в контексте после перехода.

### 1.6 get_user_bookings и MY_BOOKINGS_LIST

Спек в тексте использует `**my_bookings_formatted**`. В коде сейчас возвращается `**my_bookings_list**`.

- **Сделать:** в действии `get_user_bookings` отдавать переменную `**my_bookings_formatted**` (при необходимости оставить обратную совместимость под старым именем или заменить).

---

## 2. Library

### 2.1 Старт потока и get_user_summary

- Спека: `initial_state: AUTO_SYNC_USER_STATS`, действие `**get_user_summary**` с `payload.user_id`.
- В коде: алиас `LIBRARY_MENU` → `library.yaml/AUTO_SYNC_LIB` (состояния `AUTO_SYNC_LIB` в library.yaml нет), зарегистрировано `**get_library_stats**`.
- **Сделать:**
  - Зарегистрировать действие `**get_user_summary**`, принимающее `user_id` (или использовать контекст/текущего пользователя). Возвращать переменные для LIBRARY_HOME: `**user_firstname**`, `**campus_name**`, `**active_loans_count**`, `**overdue_count**`, `**user_status_message**` (можно вывести из тех же данных, что и для get_library_stats, плюс профиль/имя пользователя и кампус).
  - Исправить алиас в [internal/fsm/actions/library/library.go](internal/fsm/actions/library/library.go): `**LIBRARY_MENU**` → `**library.yaml/AUTO_SYNC_USER_STATS**` (или эквивалент по вашей схеме имён потоков).

### 2.2 Поиск: search_books и контракт

Спека: состояние **EXECUTE_SEARCH** вызывает `**search_books**` с payload: `query`, `category_id`, `only_available`, `page`, `limit` (например 5). Ожидаются переменные: `total_count`, `page`, `total_pages`, `formatted_book_list_with_icons`, `book_id_1`…`book_id_5`, `short_title_1`…`short_title_5`, `filter_status_text_ru`/`filter_status_text_en`, `toggle_btn_label_ru`/`toggle_btn_label_en`.

- **Сделать:**
  - Зарегистрировать действие `**search_books**` (в [internal/fsm/actions/library/library.go](internal/fsm/actions/library/library.go)) с параметрами: `**query**` (из контекста, аналог `search_query`), `**category_id**`, `**only_available**` (bool), `**page**`, `**limit**` (например 5). Реализацию можно базировать на текущем `get_books`/SearchBooks, но с пагинацией по `page`/`limit` и фильтром по доступности (при необходимости добавить/использовать запрос с условием по `book_loans`).
  - Возвращать переменные в формате спеки: `total_count`, `total_pages`, `page`, `formatted_book_list_with_icons`, `book_id_1`…`5`, `short_title_1`…`5`, `filter_status_text_ru`/`_en`, `toggle_btn_label_ru`/`_en` в зависимости от `only_available`.

### 2.3 Вспомогательные действия контекста

В library.yaml используются:

- `**set_context**` (PREPARE_SEARCH): записать в контекст, например `search_query`, `page`, `only_available`.
- `**toggle_boolean_context**` (TOGGLE_FILTER_STATE): переключить булево значение по ключу (например `only_available`) и при необходимости сбросить пагинацию.
- `**increment_context**` (NEXT_PAGE / PREV_PAGE): изменить числовое значение в контексте (например `page` на +1 / -1).

Сейчас эти действия в движке не зарегистрированы.

- **Сделать:** реализовать и зарегистрировать в общем месте (например [internal/fsm/actions/common](internal/fsm/actions/common) или в library) три действия:
  - `**set_context**`: payload — пары ключ/значение (в т.ч. подстановки из контекста, например `search_query: $context.last_input`, `page: 1`); записывать их в контекст пользователя (через возвращаемый map, который движок мержит в state.Context).
  - `**toggle_boolean_context**`: payload — `key`, опционально `reset_pagination`; инвертировать булево значение по ключу и при необходимости сбросить `page` в 1.
  - `**increment_context**`: payload — `key`, `value` (число, например +1 или -1); увеличить значение в контексте на заданное число.

Движок уже мержит результат действия в контекст ([engine.go](internal/fsm/engine.go) — `updateStateContext`), поэтому достаточно возвращать из этих действий нужные ключи/значения.

### 2.4 Триггер on_input для ввода поиска

В library.yaml в SEARCH_INPUT_MODE переход задан как `**trigger: on_input**` → PREPARE_SEARCH. В движке для input-состояний обрабатывается только `**on_valid_input**` ([internal/fsm/engine.go](internal/fsm/engine.go) ~295–301).

- **Сделать:** в том же месте при обработке input-состояния учитывать также `**trigger: "on_input"**` (как синоним перехода после ввода текста, когда валидация не требуется или уже пройдена), чтобы поиск переходил в PREPARE_SEARCH по вводу запроса.

### 2.5 get_book_details — имена переменных и поля

Спек BOOK_CARD ожидает: `**title**`, `**author**`, `**category**`, `**status_emoji**`, `**status_text_ru**` / `**status_text_en**`, `**shelf_number**`, `**description_snippet**`, `**is_available**`. Сейчас код возвращает `book_title`, `book_author`, `book_category`, `status_emoji`, `status_description`, `loan_info`, `is_available`, `selected_book_id`.

- **Сделать:**
  - Возвращать переменные в формате спеки: `**title**`, `**author**`, `**category**` (можно дублировать из текущих полей), `**status_text_ru**` и `**status_text_en**` (на базе текущего статуса), `**description_snippet**` (короткий фрагмент описания или `loan_info`), `**shelf_number**` (в схеме books полей полки нет — возвращать заглушку, например «—», или добавить поле в БД позже), `**is_available**`, `**selected_book_id**` (для последующего borrow_book).
  - При необходимости оставить старые имена для обратной совместимости или заменить их на новые.

### 2.6 get_user_loans и MY_BOOKS_LIST

Спек в тексте использует `**loans_list_formatted**`. В коде возвращается `**my_books_list_formatted**`.

- **Сделать:** в действии `get_user_loans` возвращать переменную `**loans_list_formatted**` (и при необходимости оставить/удалить старое имя).

### 2.7 BORROW_SUCCESS и title

Экран BORROW_SUCCESS использует `**{title}**` и `**{due_date}**`. `due_date` уже возвращается из `borrow_book`. `title` обычно остаётся в контексте после экрана BOOK_CARD (get_book_details возвращает title/book_title).

- **Сделать:** убедиться, что после get_book_details в контексте есть `**title**` (см. п. 2.5); при необходимости в ответе `borrow_book` при успехе тоже возвращать `**title**` из контекста или из БД, чтобы BORROW_SUCCESS всегда мог отобразить название книги.

### 2.8 Условия на кнопках (DISPLAY_RESULTS)

В DISPLAY_RESULTS переход по книге задаётся условием `id.startswith('book_')`. Движок сопоставляет ввод с кнопками по точному совпадению ID (после подстановки переменных). В YAML кнопки заданы как `id: "{book_id_1}"` и т.д. — т.е. callback уже будет вида `book_<id>`. Достаточно, чтобы **search_books** возвращал `**book_id_1`…`book_id_5**` в формате `book_<id>` (как в спеке), тогда совпадение с кнопкой и переход в FETCH_BOOK_DETAILS будут работать; в get_book_details уже извлекается ID из last_input (`book_3` → 3). Дополнительная поддержка в движке для `startswith` по переходам не обязательна, если кнопки заданы через `{book_id_1}` и т.д.

- **Сделать:** убедиться, что в DISPLAY_RESULTS в YAML кнопки слотов книг имеют `id: "{book_id_1}"` … `id: "{book_id_5}"` и `next_state: FETCH_BOOK_DETAILS` (или эквивалент по вашему YAML). Тогда изменений в движке для `startswith` не требуется.

---

## 3. Порядок внедрения (кратко)

1. **Booking:** зарегистрировать `get_dashboard_data`, расширить возвращаемые переменные дашборда (включая slot_room_name_*, slot_time_*), добавить в YAML кнопки слотов, реализовать заполнение контекста для CONFIRM_FAST (resolve_slot / on_enter), поправить create_booking (контекст + возврат room_name, time_interval), исправить имя переменной my_bookings_formatted.
2. **Library:** поправить алиас LIBRARY_MENU → AUTO_SYNC_USER_STATS, зарегистрировать get_user_summary с нужными переменными, зарегистрировать search_books с контрактом спеки, реализовать set_context / toggle_boolean_context / increment_context, в движке поддержать trigger on_input, привести get_book_details и get_user_loans к переменным спеки, при необходимости вернуть title в borrow_book/контексте.

После этого поиск и бронирование должны соответствовать новым интерфейсам и работать по сценариям из [booking.yaml](docs/specs/flows/booking.yaml) и [library.yaml](docs/specs/flows/library.yaml).