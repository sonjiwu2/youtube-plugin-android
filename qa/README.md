# Проверка VPN на устройстве

Модуль `probe` — отдельное тестовое приложение с UID `ru.youtubeplugin.probe`. Это позволяет проверять путь Android VPN → netstack → ciadpi независимо от внутреннего контрольного запроса самого плагина.

1. Соберите `:qa:probe:assembleDebug` и `:app:assembleDebug`.
2. Установите оба APK на тестовое устройство с официальным YouTube. Включите подключение в debug-плагине после установки probe.
3. Убедитесь в `dumpsys connectivity`, что VPN содержит UID YouTube и probe; UID плагина должен быть исключён.
4. Запустите `ru.youtubeplugin.probe/.ProbeActivity`. Результат: `adb shell run-as ru.youtubeplugin.probe cat files/report.txt`.
5. Проверьте рост счётчиков в `adb shell run-as ru.razgon.app cat cache/vpn-diagnostics.json`. Первый запрос до установления TUN мог пройти по старому маршруту — такой результат не подтверждает VPN. Повторите probe после запуска интерфейса.

Ожидаемые HTTP-ответы: `m.youtube.com` — 200; тест GET к API browse — 405 (метод намеренно неподходящий, подтверждает доступность TLS/HTTP); превью `i.ytimg.com` — 200; generate_204 — 204. Отдельный URL можно задать строковым extra `url`.

Далее проверьте просмотр в официальном YouTube, фон после Home/закрытия Activity, тумблер OFF, действие уведомления, плитку, потерю и возврат сети, восстановление процесса и автозапуск после reboot. Эти действия выполняйте только на выделенном тестовом устройстве.

Тестовый UID и запись приватного debug-отчёта отсутствуют в release APK. Probe не публикуется в Releases.
