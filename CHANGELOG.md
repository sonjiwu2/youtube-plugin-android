# Changelog

Формат: по этапам разработки. Версии появятся с первого релиза.

## [Не выпущено]

### Этап −1 — исследование
- Разобраны все 22 варианта `general*.bat` Flowseal, семантика nfqws/tpws, язык аргументов ciadpi.
- Составлена таблица «методика zapret → аргумент ciadpi → применимость без root» (`docs/RESEARCH.md`).

### Этап 0 — каркас
- Gradle Kotlin DSL + version catalog, AGP 9.4, встроенный Kotlin, Compose BOM.
- Минимальный модуль приложения `ru.razgon.app` (minSdk 24, compileSdk 37), отображаемое имя «Ютуб Mod».
- README, LICENSES.md, дорожная карта, заготовка CI.
