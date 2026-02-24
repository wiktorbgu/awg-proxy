# awg-proxy -- AmneziaWG для MikroTik

[English version](README_en.md)

Легковесный Docker-контейнер, который позволяет MikroTik подключаться к серверам AmneziaWG. Весь трафик шифруется нативным WireGuard-клиентом роутера, а контейнер только преобразует формат пакетов.

## Как это работает

```
MikroTik WG-клиент ──UDP──> [awg-proxy] ──UDP──> сервер AmneziaWG
   (шифрование)          (преобразование)          (обфускация)
```

Прокси заменяет заголовки пакетов, добавляет паддинг и мусорные пакеты так, чтобы сервер AmneziaWG принял трафик. Ключи и данные не затрагиваются.

Совместим с AWG v1 и v2 -- версия определяется автоматически по переменным окружения.

## Быстрый старт (конфигуратор)

1. Экспортируйте `.conf`-файл из AmneziaVPN (см. [Получение параметров AWG](#получение-параметров-awg))
2. Откройте [конфигуратор](https://amneziawg-mikrotik.github.io/awg-proxy/configurator.html)
3. Вставьте содержимое `.conf`-файла
4. Скопируйте сгенерированные команды и выполните их в терминале MikroTik

Готово. Конфигуратор работает оффлайн, данные не отправляются на сервер.

## Требования

- Сервер AmneziaWG с известными параметрами обфускации
- Файл конфигурации `.conf`, экспортированный из AmneziaVPN
- MikroTik RouterOS 7.4+ с пакетом **container**
  - **RouterOS 7.21+**: стандартные образы `awg-proxy-{arch}.tar.gz` (OCI-формат)
  - **RouterOS 7.20 и ниже**: образы `awg-proxy-{arch}-7.20-longterm.tar.gz` (Docker-формат)
  - Конфигуратор определяет версию автоматически
- Архитектура: ARM64, ARM (v7) или x86_64 ([проверить устройство](https://help.mikrotik.com/docs/spaces/ROS/pages/84901929/Container))
- Минимум 5 МБ на диске, рекомендуется 16+ МБ RAM

## Ручная установка

### 1. Включение контейнеров

Установите пакет container с [mikrotik.com](https://mikrotik.com/download), загрузите на роутер и перезагрузитесь. Затем:

```routeros
/system/device-mode/update container=yes
```

Роутер попросит подтверждение (кнопка или перезагрузка, зависит от модели).

### 2. Загрузка образа

Скачайте `awg-proxy-{arch}.tar.gz` со страницы [Releases](https://github.com/amneziawg-mikrotik/awg-proxy/releases/latest) и загрузите на роутер через Winbox или SCP. Для RouterOS 7.20 и ниже используйте файлы с суффиксом `-7.20-longterm` (Docker-формат).

Или скачайте прямо на роутер (замените URL на актуальный):

```routeros
/tool/fetch url="https://github.com/amneziawg-mikrotik/awg-proxy/releases/download/vX.X.X/awg-proxy-arm64.tar.gz" dst-path=awg-proxy-arm64.tar.gz
```

### 3. Настройка сети

```routeros
/interface/veth/add name=veth-awg-proxy address=172.18.0.2/30 gateway=172.18.0.1
/ip/address/add address=172.18.0.1/30 interface=veth-awg-proxy
/ip/firewall/nat/add chain=srcnat action=masquerade src-address=172.18.0.0/30
```

### 4. WireGuard

```routeros
/interface/wireguard/add name=wg-awg-proxy private-key="YOUR_PRIVATE_KEY" listen-port=12429
/interface/wireguard/peers/add interface=wg-awg-proxy public-key="SERVER_PUBLIC_KEY" \
    preshared-key="YOUR_PRESHARED_KEY" endpoint-address=172.18.0.2 endpoint-port=51820 \
    allowed-address=0.0.0.0/0 persistent-keepalive=25
/ip/address/add address=YOUR_TUNNEL_IP interface=wg-awg-proxy
```

Замените:
- `YOUR_PRIVATE_KEY` -- PrivateKey из `[Interface]`
- `SERVER_PUBLIC_KEY` -- PublicKey из `[Peer]`
- `YOUR_PRESHARED_KEY` -- PresharedKey из `[Peer]` (если есть)
- `YOUR_TUNNEL_IP` -- Address из `[Interface]` (например, `10.8.0.2/32`)

### 5. Переменные окружения

```routeros
/container/envs/add list=awg-proxy-env key=AWG_LISTEN value=":51820"
/container/envs/add list=awg-proxy-env key=AWG_REMOTE value="SERVER_IP:PORT"
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

Замените все значения на параметры из вашего `.conf`-файла. `AWG_CLIENT_PUB` берется автоматически из WireGuard-интерфейса.

### 6. Создание и запуск контейнера

```routeros
/container/add file=awg-proxy-arm64.tar.gz interface=veth-awg-proxy envlist=awg-proxy-env \
    hostname=awg-proxy root-dir=disk1/awg-proxy logging=yes shm-size=4M start-on-boot=yes
/container/start [find where tag~"awg-proxy"]
```

Проверьте работу:

```routeros
/container/print
/interface/wireguard/peers/print
```

Контейнер должен быть в статусе `running`, а у пира должно появиться значение `last-handshake`.

## Получение параметров AWG

1. Откройте приложение **AmneziaVPN**
2. Выберите нужное подключение
3. Нажмите **Поделиться** (Share)
4. Выберите: **Протокол**: AmneziaWG, **Формат**: AmneziaWG Format
5. Сохраните `.conf`-файл

Параметры обфускации (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1`--`H4`) находятся в секции `[Interface]`, а `Endpoint` и `PublicKey` -- в секции `[Peer]`.

## Дополнительные настройки

### Маршрутизация трафика через туннель

Конкретный хост:

```routeros
/ip/route/add dst-address=8.8.8.8/32 gateway=wg-awg-proxy
```

Подсеть:

```routeros
/ip/route/add dst-address=10.0.0.0/8 gateway=wg-awg-proxy
```

Просмотр маршрутов:

```routeros
/ip/route/print where gateway=wg-awg-proxy
```

Удаление маршрута:

```routeros
/ip/route/remove [find where dst-address="8.8.8.8/32" gateway="wg-awg-proxy"]
```

### DNS через туннель

Чтобы DNS-запросы шли через туннель, укажите DNS-сервер и добавьте маршрут к нему:

```routeros
/ip/dns/set servers=8.8.8.8,8.8.4.4
/ip/route/add dst-address=8.8.8.8/32 gateway=wg-awg-proxy
/ip/route/add dst-address=8.8.4.4/32 gateway=wg-awg-proxy
```

### Маршрутизация по address-list (продвинутое)

Для выборочной маршрутизации трафика через туннель используйте routing table и mangle rules.

Создание routing table:

```routeros
/routing/table/add disabled=no fib name=r_to_vpn
```

Маршрут по умолчанию через туннель для этой таблицы:

```routeros
/ip/route/add dst-address=0.0.0.0/0 gateway=wg-awg-proxy routing-table=r_to_vpn
```

Address-list с адресами, которые нужно направить через туннель:

```routeros
/ip/firewall/address-list/add address=8.8.8.8 list=to_vpn
/ip/firewall/address-list/add address=1.1.1.1 list=to_vpn
```

Mangle rules для маркировки трафика:

```routeros
# Пропускаем локальный трафик
/ip/firewall/mangle/add chain=prerouting action=accept dst-address=10.0.0.0/8
/ip/firewall/mangle/add chain=prerouting action=accept dst-address=172.16.0.0/12
/ip/firewall/mangle/add chain=prerouting action=accept dst-address=192.168.0.0/16

# Маркируем соединения к адресам из списка
/ip/firewall/mangle/add chain=prerouting action=mark-connection \
    dst-address-list=to_vpn connection-mark=no-mark \
    new-connection-mark=to-vpn-conn passthrough=yes

# Маркируем маршрутизацию для отмеченных соединений
/ip/firewall/mangle/add chain=prerouting action=mark-routing \
    connection-mark=to-vpn-conn new-routing-mark=r_to_vpn passthrough=yes
```

NAT для маркированного трафика:

```routeros
/ip/firewall/nat/add chain=srcnat action=masquerade routing-mark=r_to_vpn
```

Теперь весь трафик к адресам из списка `to_vpn` будет идти через туннель. Добавляйте адреса в список по мере необходимости.

## Удаление

Если установка была через конфигуратор:

```routeros
/system/script/run awg-proxy-uninstall
```

Скрипт удалит контейнер, WireGuard-интерфейс, правила NAT, маршруты, переменные окружения, восстановит DNS и удалит себя.

## Устранение неполадок

**Контейнер не запускается** -- проверьте установку пакета container (`/system/package/print`), режим устройства (`/system/device-mode/print`) и свободное место (`/system/resource/print`).

**Нет рукопожатия** -- убедитесь, что все параметры AWG (Jc, Jmin, Jmax, S1, S2, H1--H4) точно совпадают с сервером. Проверьте `AWG_REMOTE`, `AWG_SERVER_PUB` и `AWG_CLIENT_PUB`.

**Нет трафика после рукопожатия** -- проверьте правило NAT (`/ip/firewall/nat/print`), маршрутизацию и `endpoint-address` пира (должен быть `172.18.0.2`).

**Контейнер перезапускается** -- установите `AWG_LOG_LEVEL=info` и проверьте логи. Частая причина -- отсутствующие переменные окружения.

## Лицензия

MIT -- см. [LICENSE](LICENSE).
