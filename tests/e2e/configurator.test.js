// @ts-check
'use strict';

// ===========================================================================
// E2E-тесты для configurator.html — генератора RouterOS-скриптов AWG Proxy
// ===========================================================================
//
// Цель: убедиться, что configurator.html корректно генерирует RouterOS-скрипты
// для установки AWG Proxy при различных комбинациях входных параметров.
//
// Что проверяется:
//   - Правильная генерация команд для разных типов хранилища (disk1, usb, custom)
//   - Наличие/отсутствие блока проверки внешнего диска с интерактивным
//     форматированием (/terminal/inkey + /disk/format-drive)
//   - Корректное определение протокола (v1 / v1.5 / v2) и соответствующих
//     переменных окружения (S3/S4, I1-I5, H-ranges)
//   - Генерация uninstall-скрипта с очисткой pull/ для внешнего хранилища
//
// Тестовые сценарии:
//   A1 — disk1 (внутреннее хранилище): нет блока проверки диска,
//        dst-path без pull/, root-dir=disk1/awg-proxy
//   A2 — usb1-part1 (USB раздел): блок "Check storage device" присутствует,
//        filePath содержит usb1-part1/pull/, вызывается make-directory
//   A3 — usb1 (USB без раздела): блок проверки присутствует,
//        baseDisk извлекается через [:find $targetDisk "-part"]
//   A4 — Custom storage: пользовательское имя диска (sata1-part1) попадает
//        в сгенерированный скрипт
//   A5 — Протокол v1: заголовок "protocol: v1", переменные S3/S4/I1-I5
//        отсутствуют в выводе
//   A6 — Протокол v2: заголовок "protocol: v2", присутствуют S3/S4, I1-I5,
//        H-значения содержат диапазоны (MIN-MAX)
//   A7 — Uninstall для USB: внутри source={...} uninstall-скрипта есть
//        очистка директории pull/
//   A8 — Интерактивное форматирование: для внешнего хранилища скрипт содержит
//        /terminal/inkey (ожидание подтверждения) и /disk/format-drive
//
// Безопасность (S1-S6):
//   Проверяют, что сгенерированный скрипт не содержит опасных для RouterOS
//   команд — ни в основном скрипте, ни в uninstall-скрипте.
//   Тестируются оба сценария (disk1 и USB), т.к. генерируются разные ветки кода.
//
//   S1 — Нет команд управления пользователями и доступом: /user, /password,
//        /ip/service, /ip/ssh, /radius, /snmp, /certificate
//   S2 — Нет деструктивных системных команд: /system/reset, /system/reboot,
//        /system/shutdown, /system/backup, /system/license
//   S3 — Нет средств перехвата трафика: /tool/sniffer, /ip/socks, /ip/proxy,
//        /ip/firewall/filter, /ip/firewall/mangle, /ip/firewall/raw,
//        /ip/firewall/layer7
//   S4 — Нет бэкдоров и утечки данных: /system/scheduler, /export,
//        /tool/e-mail, /tool/sms; также нет VPN-серверов и туннелей
//        (/ppp, pptp, l2tp, sstp, ovpn, gre, eoip, ipsec)
//   S5 — /tool/fetch использует только доверенный URL GitHub-репозитория,
//        без upload-параметров
//   S6 — NAT-правила только srcnat masquerade — нет dst-nat, redirect, netmap
//
// Запуск:
//   cd tests/e2e && npx playwright test configurator.test.js
//
// ===========================================================================

const { test, expect } = require('@playwright/test');
const path = require('path');
const fs = require('fs');

// ---------------------------------------------------------------------------
// Test data
// ---------------------------------------------------------------------------

const V1_CONF = `[Interface]
Address = 10.8.1.2/32
DNS = 1.1.1.1, 8.8.8.8
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Jc = 4
Jmin = 10
Jmax = 50
S1 = 100
S2 = 80
H1 = 1234567890
H2 = 1234567891
H3 = 1234567892
H4 = 1234567893

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
PresharedKey = CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 203.0.113.1:51820
PersistentKeepalive = 25`;

const V2_CONF_EMBEDDED = `[Interface]
Address = 10.8.1.2/32
DNS = 1.1.1.1
PrivateKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Jc = 4
Jmin = 10
Jmax = 50
S1 = 100
S2 = 80
S3 = 50
S4 = 60
H1 = 1000000000-2000000000
H2 = 1000000001-2000000001
H3 = 1000000002-2000000002
H4 = 1000000003-2000000003
I1 = <r 32>
I2 = <r 32>
I3 = <r 32>
I4 = <r 32>
I5 = <r 32>

[Peer]
PublicKey = BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=
AllowedIPs = 0.0.0.0/0
Endpoint = 203.0.113.1:51820`;

// Load real v2 config from env var if available, else fall back to embedded
function getV2Conf() {
    const confPath = process.env.TEST_CONF_PATH;
    if (confPath) {
        try {
            const data = fs.readFileSync(confPath, 'utf8');
            if (data.trim()) return data;
        } catch (_) {
            // file not found or unreadable — fall through to embedded
        }
    }
    return V2_CONF_EMBEDDED;
}

// ---------------------------------------------------------------------------
// Page URL
// ---------------------------------------------------------------------------

const CONFIGURATOR_PATH = path.resolve(__dirname, '../../docs/configurator.html');
const PAGE_URL = 'file://' + CONFIGURATOR_PATH.replace(/\\/g, '/');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Fill the conf textarea, select storage, and click Generate.
 * @param {import('@playwright/test').Page} page
 * @param {string} conf
 * @param {string} storage  storage-select value, or '__custom'
 * @param {string|undefined} customStorage  value for #storage-custom when storage === '__custom'
 */
async function generate(page, conf, storage, customStorage) {
    await page.locator('#conf-input').fill(conf);

    await page.locator('#storage-select').selectOption(storage);

    if (storage === '__custom' && customStorage) {
        await page.locator('#storage-custom').fill(customStorage);
    }

    await page.getByRole('button', { name: 'Generate commands' }).click();
}

/**
 * Return the data-plain attribute of #output (raw text).
 * @param {import('@playwright/test').Page} page
 * @returns {Promise<string>}
 */
async function getOutput(page) {
    return (await page.locator('#output').getAttribute('data-plain')) || '';
}

/**
 * Return the data-plain attribute of #uninstall-output (raw text).
 * @param {import('@playwright/test').Page} page
 * @returns {Promise<string>}
 */
async function getUninstallOutput(page) {
    return (await page.locator('#uninstall-output').getAttribute('data-plain')) || '';
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.beforeEach(async ({ page }) => {
    await page.goto(PAGE_URL);
});

// A1: disk1 storage (default) — no storage-check block, simple dst-path, correct root-dir
test('A1: disk1 storage - no check block, plain dst-path, root-dir=disk1/awg-proxy', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const out = await getOutput(page);

    expect(out).not.toContain('Check storage device');
    // For disk1, filePath is set to $file (no pull/ subdir)
    expect(out).not.toContain('/pull/');
    // fetch always uses $filePath; root-dir must reference disk1
    expect(out).toContain('dst-path=$filePath');
    expect(out).toContain('root-dir=disk1/awg-proxy');
});

// A2: usb1-part1 storage — storage-check block present, pull/ path, make-directory
test('A2: usb1-part1 storage - check block, pull/ path, make-directory', async ({ page }) => {
    await generate(page, V1_CONF, 'usb1-part1');
    const out = await getOutput(page);

    expect(out).toContain('Check storage device');
    expect(out).toContain('usb1-part1/pull/');
    expect(out).toContain('make-directory');
});

// A3: usb1 storage — storage-check block present, baseDisk extraction via [:find $targetDisk "-part"]
test('A3: usb1 storage - check block with baseDisk extraction present', async ({ page }) => {
    await generate(page, V1_CONF, 'usb1');
    const out = await getOutput(page);

    expect(out).toContain('Check storage device');
    // The check block must contain the baseDisk extraction expression
    expect(out).toContain('[:find $targetDisk "-part"]');
});

// A4: Custom storage — custom disk name flows through to output
test('A4: custom storage sata1-part1', async ({ page }) => {
    await generate(page, V1_CONF, '__custom', 'sata1-part1');
    const out = await getOutput(page);

    expect(out).toContain('sata1-part1');
});

// A5: v1 protocol — correct badge, no S3/S4/I1-I5 env vars
test('A5: v1 protocol - correct label, no v2-only env vars', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const out = await getOutput(page);

    expect(out).toContain('protocol: v1');
    expect(out).not.toContain('AWG_S3');
    expect(out).not.toContain('AWG_S4');
    expect(out).not.toContain('AWG_I1');
    expect(out).not.toContain('AWG_I2');
    expect(out).not.toContain('AWG_I3');
    expect(out).not.toContain('AWG_I4');
    expect(out).not.toContain('AWG_I5');
});

// A6: v2 protocol — correct badge, S3/S4/I1-I5 env vars present, H values contain ranges
test('A6: v2 protocol - correct label, S3/S4 and I1-I5 env vars, H range values', async ({ page }) => {
    const v2Conf = getV2Conf();
    await generate(page, v2Conf, 'disk1');
    const out = await getOutput(page);

    expect(out).toContain('protocol: v2');
    expect(out).toContain('AWG_S3');
    expect(out).toContain('AWG_S4');
    expect(out).toContain('AWG_I1');
    expect(out).toContain('AWG_I2');
    expect(out).toContain('AWG_I3');
    expect(out).toContain('AWG_I4');
    expect(out).toContain('AWG_I5');

    // H values should retain the range notation (e.g. "1000000000-2000000000")
    expect(out).toMatch(/AWG_H1.*-/);
    expect(out).toMatch(/AWG_H2.*-/);
    expect(out).toMatch(/AWG_H3.*-/);
    expect(out).toMatch(/AWG_H4.*-/);
});

// A7: Uninstall script contains cleanup of pull/ for USB storage
test('A7: uninstall script cleans up pull/ for USB storage', async ({ page }) => {
    await generate(page, V1_CONF, 'usb1-part1');
    const out = await getOutput(page);

    // Extract uninstall script source (between 'source={' and the closing '}')
    const sourceStart = out.indexOf('source={');
    expect(sourceStart).toBeGreaterThan(-1);
    const sourceEnd = out.indexOf('\n}', sourceStart);
    const uninstallSource = out.slice(sourceStart, sourceEnd);

    // Uninstall script must clean up pull/ directory for USB storage
    expect(uninstallSource).toContain('usb1-part1/pull');
});

// A8: Format prompt for external storage — /terminal/inkey and /disk/format-drive present
test('A8: format prompt for external storage', async ({ page }) => {
    await generate(page, V1_CONF, 'usb1-part1');
    const out = await getOutput(page);

    expect(out).toContain('/terminal/inkey');
    expect(out).toContain('/disk/format-drive');
});

// ---------------------------------------------------------------------------
// Security tests
// ---------------------------------------------------------------------------

// Опасные команды RouterOS, которых не должно быть в сгенерированном скрипте.
// Разбиты по категориям угроз.

const DANGEROUS_PATTERNS = {
    // Управление пользователями и доступом — бэкдор-аккаунты, открытие сервисов
    'user/account manipulation': [
        '/user/',
        '/password',
        '/ip/service/',
        '/ip/ssh/',
        '/radius/',
        '/snmp/',
        '/certificate/',
    ],
    // Деструктивные системные команды — сброс, перезагрузка, утечка бэкапа
    'destructive system commands': [
        '/system/reset',
        '/system/reboot',
        '/system/shutdown',
        '/system/backup',
        '/system/restore',
        '/system/license/',
    ],
    // Перехват и манипуляция трафиком
    'traffic interception': [
        '/tool/sniffer',
        '/ip/socks/',
        '/ip/proxy/',
        '/ip/firewall/filter/',
        '/ip/firewall/mangle/',
        '/ip/firewall/raw/',
        '/ip/firewall/layer7',
    ],
    // Персистентные бэкдоры и утечка данных
    'backdoors and data exfiltration': [
        '/system/scheduler/',
        '/tool/e-mail/',
        '/tool/sms/',
        '/export',
    ],
    // VPN-серверы и туннели — скрытые каналы связи
    'rogue VPN/tunnel': [
        '/ppp/',
        'pptp-server',
        'l2tp-server',
        'sstp-server',
        'ovpn-server',
        '/interface/gre/',
        '/interface/eoip/',
        '/ip/ipsec/',
    ],
    // Маршрутизация на уровне протоколов — перехват BGP/OSPF
    'routing protocol manipulation': [
        '/routing/bgp',
        '/routing/ospf',
        '/routing/filter',
    ],
};

// Все паттерны одним плоским списком для быстрой проверки
const ALL_DANGEROUS = Object.values(DANGEROUS_PATTERNS).flat();

/**
 * Извлечь все строки, содержащие RouterOS-команды (начинаются с /),
 * исключая комментарии (# ...) и строки внутри :put (информационный вывод).
 * Это позволяет не срабатывать на текст подсказок типа :put "/ip/route/add..."
 */
function extractExecutableLines(output) {
    return output.split('\n').filter(function(line) {
        var trimmed = line.trim();
        // Пропускаем комментарии
        if (trimmed.startsWith('#')) return false;
        // Пропускаем :put — это вывод текста пользователю, не исполняемые команды
        if (trimmed.startsWith(':put')) return false;
        return true;
    });
}

// S1: Нет команд управления пользователями и доступом
test('S1: no user/account/service manipulation commands', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const outDisk1 = await getOutput(page);

    await generate(page, V1_CONF, 'usb1-part1');
    const outUsb = await getOutput(page);

    for (const out of [outDisk1, outUsb]) {
        const lines = extractExecutableLines(out);
        const text = lines.join('\n');
        for (const pat of DANGEROUS_PATTERNS['user/account manipulation']) {
            expect(text, 'found dangerous pattern: ' + pat).not.toContain(pat);
        }
    }
});

// S2: Нет деструктивных системных команд
test('S2: no destructive system commands (reset, reboot, backup)', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const outDisk1 = await getOutput(page);

    await generate(page, V1_CONF, 'usb1-part1');
    const outUsb = await getOutput(page);

    for (const out of [outDisk1, outUsb]) {
        const lines = extractExecutableLines(out);
        const text = lines.join('\n');
        for (const pat of DANGEROUS_PATTERNS['destructive system commands']) {
            expect(text, 'found dangerous pattern: ' + pat).not.toContain(pat);
        }
    }
});

// S3: Нет средств перехвата трафика
test('S3: no traffic interception (sniffer, socks, proxy, filter, mangle)', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const outDisk1 = await getOutput(page);

    await generate(page, V1_CONF, 'usb1-part1');
    const outUsb = await getOutput(page);

    for (const out of [outDisk1, outUsb]) {
        const lines = extractExecutableLines(out);
        const text = lines.join('\n');
        for (const pat of DANGEROUS_PATTERNS['traffic interception']) {
            expect(text, 'found dangerous pattern: ' + pat).not.toContain(pat);
        }
    }
});

// S4: Нет бэкдоров, утечки данных, VPN-серверов и туннелей
test('S4: no backdoors, data exfil, rogue VPN/tunnels, routing manipulation', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const outDisk1 = await getOutput(page);

    await generate(page, V1_CONF, 'usb1-part1');
    const outUsb = await getOutput(page);

    const categories = [
        'backdoors and data exfiltration',
        'rogue VPN/tunnel',
        'routing protocol manipulation',
    ];
    for (const out of [outDisk1, outUsb]) {
        const lines = extractExecutableLines(out);
        const text = lines.join('\n');
        for (const cat of categories) {
            for (const pat of DANGEROUS_PATTERNS[cat]) {
                expect(text, 'found dangerous pattern: ' + pat).not.toContain(pat);
            }
        }
    }
});

// S5: /tool/fetch только с доверенным GitHub URL, без upload
test('S5: fetch only from trusted GitHub URL, no upload', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const outDisk1 = await getOutput(page);

    await generate(page, V1_CONF, 'usb1-part1');
    const outUsb = await getOutput(page);

    const TRUSTED_URL_PREFIX = 'https://github.com/amneziamikrotikwg/awg-proxy/';

    for (const out of [outDisk1, outUsb]) {
        const lines = extractExecutableLines(out);

        // Найти все строки с /tool/fetch
        const fetchLines = lines.filter(l => l.includes('/tool/fetch'));
        expect(fetchLines.length).toBeGreaterThan(0);

        for (const line of fetchLines) {
            // Должен использовать переменную $url (которая задана через trusted URL)
            expect(line).toContain('url=$url');
            // Не должен содержать upload
            expect(line).not.toContain('upload');
            expect(line).not.toContain('mode=https');
        }

        // Проверить что определение $url использует только доверенный домен
        const urlDef = lines.find(l => l.includes(':local url'));
        expect(urlDef).toBeDefined();
        expect(urlDef).toContain(TRUSTED_URL_PREFIX);
    }
});

// S6: NAT-правила только srcnat masquerade — нет dst-nat, redirect, netmap
test('S6: NAT rules only srcnat masquerade, no dst-nat/redirect/netmap', async ({ page }) => {
    await generate(page, V1_CONF, 'disk1');
    const outDisk1 = await getOutput(page);

    await generate(page, V1_CONF, 'usb1-part1');
    const outUsb = await getOutput(page);

    for (const out of [outDisk1, outUsb]) {
        const lines = extractExecutableLines(out);

        // Найти все строки добавления NAT-правил
        const natLines = lines.filter(l => l.includes('/ip/firewall/nat/add'));

        expect(natLines.length).toBeGreaterThan(0);

        for (const line of natLines) {
            // Только chain=srcnat
            expect(line).toContain('chain=srcnat');
            // Только action=masquerade
            expect(line).toContain('action=masquerade');
            // Нет опасных действий
            expect(line).not.toContain('dst-nat');
            expect(line).not.toContain('redirect');
            expect(line).not.toContain('netmap');
        }
    }
});
