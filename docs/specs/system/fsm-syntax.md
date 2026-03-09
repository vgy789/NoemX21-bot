# Спецификация FSM Схем (FlowSpec)

Документ описывает синтаксис YAML-конфигураций для управления состояниями (FSM) NoemX21-bot.

## 1. FlowSpec (Корень файла)
| Поле | Тип | Описание |
| :--- | :--- | :--- |
| `initial_state` | `string` | ID состояния при первом входе в поток. |
| `states` | `map[string]State` | Карта всех доступных состояний в данном файле. |

---

## 2. State (Состояние)
| Поле | Тип | Описание |
| :--- | :--- | :--- |
| `type` | `enum` | **interactive** (кнопки), **system** (сквозное), **input** (текст), **final**. |
| `description` | `string` | Техническое описание для логов. |
| `interface` | `Interface` | Визуальная часть (текст, кнопки, медиа). |
| `logic` | `Logic` | Основное действие (только для `system`). |
| `validation` | `Validation` | Правила проверки ввода (только для `input`). |
| `transitions` | `[]Transition` | Правила перехода в следующее состояние. |
| `on_enter` | `[]Logic` | Список действий при входе в состояние. |
| `on_exit` | `[]Logic` | Список действий при выходе из состояния. |

---

## 3. Interface (Интерфейс)
| Поле | Тип | Описание |
| :--- | :--- | :--- |
| `text` | `map[ru/en]` | Локализованные сообщения. Поддерживают плейсхолдеры `{name}`. |
| `image` | `string` | (Опционально) Путь к медиа-файлу или URL. |
| `error_invalid` | `map[ru/en]` | Сообщение при ошибке валидации (тип `input`). |
| `buttons` | `[]Button` | Массив кнопок. |

### Button (Кнопка)
| Поле | Тип | Описание |
| :--- | :--- | :--- |
| `id` | `string` | Уникальный ID кнопки (callback_data). |
| `label` | `string/map` | Текст кнопки (строка или локализованная карта). |
| `next_state` | `string` | Цель: `STATE` (текущий файл) или `other.yaml/STATE`. |
| `url` | `string` | URL для перехода (взаимно исключает `next_state`). |
| `row` | `int` | Группировка кнопок в одну строку (одинаковый ID ряда). |
| `condition` | `string` | Условие отображения кнопки (например, `is_admin == true`). Если ложно — кнопка скрывается. |
| `action` | `string` | Действие, выполняемое мгновенно при нажатии. Значение `none` делает кнопку декоративной (пустышкой, например, для отображения страниц `1/5`). |

---

## 4. Logic & Transitions
### Logic (Действия)
| Поле | Тип | Описание |
| :--- | :--- | :--- |
| `action` | `string` | Имя Go-обработчика (Actions Registry). |
| `payload` | `map[string]any`| Параметры действия. Поддерживают `$context.key`. |

### Transition (Переходы)
| Поле | Тип | Описание |
| :--- | :--- | :--- |
| `next_state` | `string` | Целевое состояние. |
| `condition` | `string` | Логическое выражение (для `system`). Пример: `status == 'ok'`. Если отсутствует — срабатывает безусловно (как `else`). |
| `trigger` | `string` | Событие (для `input`). Стандарт: `on_valid_input` или `on_input`. |
| `action` | `string` | Выполнение действия *во время* перехода. |

---

## 5. Примеры реализации

### Сложный интерактив (с условием на кнопке)
```yaml
MENU_MAIN:
  type: interactive
  interface:
    text:
      ru: "Выберите действие, {first_name}:"
    buttons:
      - id: open_admin
        label: "🛡 Админка"
        condition: "is_admin == true"
        next_state: ADMIN_PANEL
      - id: profile
        label: "👤 Профиль"
        next_state: PROFILE_VIEW
        row: 1
```

### Обработка ввода (Input)
```yaml
INPUT_AGE:
  type: input
  validation:
    regex: "^[0-9]{1,2}$"
  interface:
    text:
      ru: "Сколько вам лет?"
    error_invalid:
      ru: "Пожалуйста, введите корректный возраст (0-99)."
  transitions:
    - trigger: on_valid_input
      next_state: SAVE_AND_CONTINUE
```

### Системное состояние (Switch с on_enter и fallback)
```yaml
CHECK_DUPLICATE:
  type: system
  on_enter:
    - action: log_duplicate_check
      payload:
        user_id: $context.user_id
  logic:
    action: "db_check_open_prr"
    payload:
      project_id: $context.selected_project_id
  transitions:
    - condition: "has_open_prr == true"
      next_state: ERROR_PRR_ALREADY_EXISTS
    - next_state: INPUT_TIME_AVAILABILITY  # Дефолтный переход (else)
```

### Декоративные кнопки и URL
```yaml
PAGINATION_EXAMPLE:
  type: interactive
  interface:
    text:
      ru: "Выбор проекта"
    buttons:
      - id: prev
        label: "⬅️ Назад"
        condition: "has_prev == true"
        action: prev_page
        next_state: PAGINATION_EXAMPLE
      - id: page_indicator
        label: "{current_page} / {total_pages}"
        action: none  # Кнопка ничего не делает при клике
      - id: contact
        label: "💬 Написать"
        url: "https://t.me/{username}"
        action: open_url
```
