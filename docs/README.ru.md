# awg-proxy -- UDP-прокси AmneziaWG для MikroTik

Легковесный Docker-контейнер, преобразующий стандартный трафик WireGuard в формат, совместимый с AmneziaWG, что позволяет маршрутизаторам MikroTik подключаться к серверам AmneziaWG и обходить DPI.

## Содержание

- [Требования](#требования)
- [Быстрый старт](#быстрый-старт)
- [Установка](#установка)
- [Проверка](#проверка)
- [Справочник по конфигурации](#справочник-по-конфигурации)
- [Получение параметров AWG](#получение-параметров-awg)
- [Удаление](#удаление)
- [Сборка из исходников](#сборка-из-исходников)
- [Устранение неполадок](#устранение-неполадок)

## Принцип работы

```
WG-клиент MikroTik ──UDP──► [контейнер awg-proxy] ──UDP──► Сервер AmneziaWG
  (нативная криптография)    (преобразование пакетов)       (видит валидный AWG)
```

MikroTik выполняет всю криптографию WireGuard нативно, используя встроенный WG-клиент. Прокси располагается между маршрутизатором и сервером AmneziaWG, выполняя только преобразование структуры пакетов:

- **Исходящие (WG в AWG):** заменяет стандартные заголовки типов сообщений WireGuard на значения AmneziaWG (H1--H4), добавляет случайный паддинг в начало пакетов рукопожатия (S1/S2 байт), отправляет мусорные пакеты перед инициацией рукопожатия (Jc пакетов размером от Jmin до Jmax байт), пересчитывает MAC1 с публичным ключом сервера AWG.
- **Входящие (AWG в WG):** выполняет обратную замену типов, удаляет паддинг из пакетов рукопожатия, молча отбрасывает мусорные пакеты, пересчитывает MAC1 с публичным ключом WG-клиента.

Криптографические ключи и данные туннеля не изменяются. Прокси полностью прозрачен для протокольного уровня WireGuard.

## Быстрый старт

1. Экспортируйте `.conf`-файл AmneziaWG (см. [Получение параметров AWG](#получение-параметров-awg))
2. Откройте **[оффлайн-конфигуратор](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html)**
3. Вставьте содержимое `.conf`-файла и скопируйте сгенерированные команды
4. Выполните команды на вашем маршрутизаторе MikroTik через терминал

## Требования

- **Сервер AmneziaWG** -- работающий сервер с известными параметрами обфускации
- **Файл конфигурации** (`.conf`) -- экспортированный из AmneziaVPN (см. [Получение параметров AWG](#получение-параметров-awg))
- **MikroTik RouterOS 7.4+** с установленным пакетом **container**
- **Поддерживаемые архитектуры**: ARM64, ARM (v7) или x86\_64
  ([проверьте своё устройство](https://help.mikrotik.com/docs/spaces/ROS/pages/47579139/Container))
- Включённый режим устройства: `/system/device-mode/update container=yes`
- Минимум 5 МБ свободного места на диске, рекомендуется 16+ МБ свободной RAM

## Установка

### Шаг 1: Включение пакета container

Если пакет container ещё не установлен, скачайте его с [mikrotik.com](https://mikrotik.com/download) для вашей версии RouterOS и архитектуры, загрузите на роутер и перезагрузитесь. Затем включите режим устройства:

```routeros
/system/device-mode/update container=yes
```

После этой команды роутер попросит физически нажать кнопку подтверждения (или перезагрузится автоматически, в зависимости от модели).

### Выберите способ настройки

**Вариант А: [Оффлайн-конфигуратор](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html) (рекомендуется)**

Вставьте содержимое вашего `.conf`-файла AmneziaWG и получите готовые команды MikroTik. Скопируйте и выполните их на роутере, затем перейдите к разделу [Проверка](#проверка).

**Вариант Б: Ручная настройка**

Следуйте шагам 2--7 ниже для пошаговой настройки.

### Шаг 2: Загрузка образа

Скачайте `awg-proxy-{arch}.tar.gz` из раздела [GitHub Releases](https://github.com/amneziawg-mikrotik/awg-proxy/releases/latest) (выберите arm64, arm или amd64 в соответствии с архитектурой вашего маршрутизатора) и загрузите файл на маршрутизатор MikroTik.

Альтернативно, можно скачать образ прямо на роутер командой RouterOS (замените URL на актуальный из раздела releases):

```routeros
# /tool/fetch url="https://github.com/amneziawg-mikrotik/awg-proxy/releases/download/vX.X.X/awg-proxy-arm64.tar.gz" dst-path=awg-proxy-arm64.tar.gz
```

### Шаг 3: Настройка сети

Создайте виртуальный Ethernet-интерфейс для контейнера и настройте NAT:

```routeros
# Создание виртуального Ethernet-интерфейса для контейнера
/interface/veth/add name=veth-awg-proxy address=172.18.0.2/30 gateway=172.18.0.1

# Назначение IP-адреса на хостовую сторону veth-пары
/ip/address/add address=172.18.0.1/30 interface=veth-awg-proxy

# Правило NAT для доступа контейнера в интернет
/ip/firewall/nat/add chain=srcnat action=masquerade src-address=172.18.0.0/30
```

### Шаг 4: Настройка WireGuard

Создайте WireGuard-интерфейс, добавьте пира, указывающего на контейнер, и назначьте туннельный IP-адрес:

```routeros
# Создание WireGuard-интерфейса
/interface/wireguard/add name=wg-awg-proxy private-key="YOUR_PRIVATE_KEY" listen-port=12429

# Добавление пира (endpoint указывает на контейнер awg-proxy)
/interface/wireguard/peers/add interface=wg-awg-proxy public-key="SERVER_PUBLIC_KEY" preshared-key="YOUR_PRESHARED_KEY" endpoint-address=172.18.0.2 endpoint-port=51820 allowed-address=0.0.0.0/0 persistent-keepalive=25

# Назначение туннельного IP-адреса интерфейсу
/ip/address/add address=YOUR_TUNNEL_IP interface=wg-awg-proxy
```

Замените `YOUR_PRIVATE_KEY` на приватный ключ из `[Interface]` PrivateKey, `SERVER_PUBLIC_KEY` на публичный ключ сервера из `[Peer]` PublicKey, `YOUR_PRESHARED_KEY` на preshared-ключ (если есть), `YOUR_TUNNEL_IP` на туннельный IP из `[Interface]` Address (например, `10.8.0.2/32`). Маршруты настраиваются отдельно по необходимости.

### Шаг 5: Переменные окружения

`AWG_CLIENT_PUB` автоматически считывается из WireGuard-интерфейса, созданного на предыдущем шаге -- вычислять его вручную не нужно.

```routeros
# Переменные окружения контейнера (параметры обфускации AWG)
/container/envs/add list=awg-proxy-env key=AWG_LISTEN value=":51820"
/container/envs/add list=awg-proxy-env key=AWG_REMOTE value="YOUR_SERVER:PORT"
/container/envs/add list=awg-proxy-env key=AWG_JC value="5"
/container/envs/add list=awg-proxy-env key=AWG_JMIN value="30"
/container/envs/add list=awg-proxy-env key=AWG_JMAX value="500"
/container/envs/add list=awg-proxy-env key=AWG_S1 value="20"
/container/envs/add list=awg-proxy-env key=AWG_S2 value="20"
/container/envs/add list=awg-proxy-env key=AWG_H1 value="1234567890"
/container/envs/add list=awg-proxy-env key=AWG_H2 value="1234567891"
/container/envs/add list=awg-proxy-env key=AWG_H3 value="1234567892"
/container/envs/add list=awg-proxy-env key=AWG_H4 value="1234567893"
/container/envs/add list=awg-proxy-env key=AWG_SERVER_PUB value="SERVER_PUBLIC_KEY"
/container/envs/add list=awg-proxy-env key=AWG_CLIENT_PUB value=[/interface/wireguard/get [find name=wg-awg-proxy] public-key]
```

Замените `YOUR_SERVER:PORT` на адрес и порт вашего сервера AmneziaWG. Замените все значения H1--H4, S1, S2, Jc, Jmin, Jmax на реальные параметры из вашей конфигурации AmneziaWG. `AWG_SERVER_PUB` -- публичный ключ сервера (из `[Peer]` PublicKey в вашем .conf-файле).

### Шаг 6: Создание контейнера

```routeros
/container/add file=awg-proxy-arm64.tar.gz interface=veth-awg-proxy envlist=awg-proxy-env hostname=awg-proxy root-dir=disk1/awg-proxy logging=yes shm-size=4M start-on-boot=yes
```

### Шаг 7: Запуск

```routeros
/container/start [find where tag~"awg-proxy"]
```

## Проверка

После запуска убедитесь, что контейнер работает и туннель поднялся:

```routeros
/container/print
/interface/wireguard/print
/interface/wireguard/peers/print
/ping 172.18.0.2
```

Контейнер должен быть в состоянии `running`. У пира WireGuard поле `last-handshake` должно обновиться в течение нескольких секунд после запуска.

## Справочник по конфигурации

Вся настройка выполняется через переменные окружения, передаваемые контейнеру.

| Переменная | Обязательна | По умолчанию | Описание |
|---|---|---|---|
| `AWG_LISTEN` | Да | -- | Адрес прослушивания, например `:51820` |
| `AWG_REMOTE` | Да | -- | Адрес сервера AmneziaWG (`хост:порт`) |
| `AWG_JC` | Да | -- | Количество мусорных пакетов перед инициацией рукопожатия |
| `AWG_JMIN` | Да | -- | Минимальный размер мусорного пакета в байтах |
| `AWG_JMAX` | Да | -- | Максимальный размер мусорного пакета в байтах |
| `AWG_S1` | Да | -- | Случайный паддинг перед пакетом инициации рукопожатия (байт) |
| `AWG_S2` | Да | -- | Случайный паддинг перед пакетом ответа рукопожатия (байт) |
| `AWG_H1` | Да | -- | Подменный тип сообщения для инициации рукопожатия |
| `AWG_H2` | Да | -- | Подменный тип сообщения для ответа рукопожатия |
| `AWG_H3` | Да | -- | Подменный тип сообщения для cookie reply |
| `AWG_H4` | Да | -- | Подменный тип сообщения для транспортных данных |
| `AWG_SERVER_PUB` | Да | -- | Публичный ключ сервера AWG (base64), используется для пересчёта MAC1 в исходящих пакетах рукопожатия |
| `AWG_CLIENT_PUB` | Да | -- | Публичный ключ WG-клиента (base64), берётся автоматически из WG-интерфейса (см. шаг 5) |
| `AWG_TIMEOUT` | Нет | `180` | Таймаут неактивности в секундах до переподключения |
| `AWG_LOG_LEVEL` | Нет | `info` | Уровень логирования: `none`, `error` или `info` |

## Получение параметров AWG

Значения Jc, Jmin, Jmax, S1, S2, H1--H4 должны точно совпадать с конфигурацией вашего сервера AmneziaWG. Чтобы их получить:

### Экспорт из AmneziaVPN

1. Откройте приложение **AmneziaVPN**
2. Выберите нужное подключение
3. Нажмите **Поделиться** (Share)
4. Выберите: **Протокол**: AmneziaWG, **Формат**: AmneziaWG Format
5. Сохраните полученный `.conf`-файл

### Чтение параметров

1. Откройте экспортированный `.conf`-файл в текстовом редакторе.
2. Параметры обфускации находятся в секции `[Interface]`:
   ```ini
   [Interface]
   Jc = 5
   Jmin = 30
   Jmax = 500
   S1 = 20
   S2 = 20
   H1 = 1234567890
   H2 = 1234567891
   H3 = 1234567892
   H4 = 1234567893
   ```
3. Значение `Endpoint` из секции `[Peer]` используется как `AWG_REMOTE`.
4. Значение `PublicKey` из секции `[Peer]` используется как `AWG_SERVER_PUB`.
5. `AWG_CLIENT_PUB` берётся автоматически из WireGuard-интерфейса (см. шаг 5).

Также можно воспользоваться [оффлайн-конфигуратором](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html) -- вставьте содержимое .conf-файла и получите готовые команды MikroTik.

## Удаление

Скрипт удаления создаётся автоматически при установке через конфигуратор.
Для удаления awg-proxy выполните:

```routeros
/system/script/run awg-proxy-uninstall
```

Скрипт удаляет контейнер, WireGuard-интерфейс, правила NAT, маршруты,
переменные окружения, восстанавливает предыдущие настройки DNS и удаляет себя.

## Сборка из исходников

Требуется Go 1.25+ и Docker с поддержкой buildx.

```bash
make build          # Сборка локального бинарника
make test           # Запуск тестов с детектором гонок
make docker-arm64   # Docker-образ для ARM64 (устройства MikroTik ARM64)
make docker-arm     # Docker-образ для ARM v7
make docker-amd64   # Docker-образ для x86_64
make docker-all     # Сборка для всех архитектур
```

Docker-сборка создаёт минимальный образ на основе scratch, содержащий единственный статически скомпонованный бинарный файл.

## Устранение неполадок

**Контейнер не запускается**
- Убедитесь, что пакет container установлен: `/system/package/print`
- Проверьте, что режим устройства включён: `/system/device-mode/print`
- Проверьте свободное место на диске: `/system/resource/print`

**Таймаут рукопожатия (соединение не устанавливается)**
- Убедитесь, что все параметры AWG (Jc, Jmin, Jmax, S1, S2, H1--H4) точно совпадают с конфигурацией сервера. Даже одно несовпадающее значение приведёт к невозможности установить рукопожатие.
- Проверьте, что `AWG_REMOTE` указывает на правильный адрес и порт сервера.
- Убедитесь, что `AWG_SERVER_PUB` и `AWG_CLIENT_PUB` заданы корректно -- неверные ключи приведут к невалидному MAC1 и отбросу пакетов сервером или клиентом.
- Убедитесь, что контейнер может достичь сервера -- правило NAT masquerade должно быть настроено.

**Нет трафика после успешного рукопожатия**
- Проверьте наличие правила NAT: `/ip/firewall/nat/print`
- Проверьте маршрутизацию на MikroTik -- трафик к пиру WireGuard должен идти через прокси.
- Убедитесь, что `endpoint-address` пира WireGuard установлен на IP контейнера (`172.18.0.2`).

**Контейнер перезапускается в цикле**
- Проверьте статус контейнера: `/container/print`
- Установите `AWG_LOG_LEVEL` в `info` для просмотра подробных логов прокси.
- Частая причина: отсутствующие или некорректные переменные окружения. Все обязательные переменные должны быть заданы.

## Лицензия

MIT -- подробности в файле [LICENSE](../LICENSE).
