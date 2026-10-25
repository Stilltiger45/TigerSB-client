# Как выпустить версию

Приложения собирает GitHub Actions. Компьютер со средой разработки не нужен.

## Выпуск

1. Откройте [Releases → Draft a new release](https://github.com/Stilltiger45/TigerSB-client/releases/new).
2. В поле **Choose a tag** впишите новый номер версии с буквой `v`, например `v0.1.0`, и выберите **Create new tag on publish**.
3. Нажмите **Publish release**.

Через 10–15 минут к релизу сами прикрепятся файлы:

| Файл | Для кого |
|---|---|
| `TigerSB-0.1.0-android.apk` | почти все телефоны на Android 7+ |
| `TigerSB-0.1.0-android-old-phones.apk` | старые 32-битные телефоны |

Ход сборки виден на вкладке **Actions**. Каждая следующая версия должна иметь номер больше предыдущей: `v0.1.1`, `v0.2.0` и так далее.

Тестовую сборку без релиза можно скачать из любого pull request: вкладка **Actions** → нужный запуск → артефакт `TigerSB-android`.

## Ключ подписи Android (один раз)

Android устанавливает обновление поверх старой версии, только если обе подписаны одним ключом. Без своего ключа каждая сборка подписывается случайным отладочным, и перед обновлением приложение придётся удалять.

1. Создайте ключ (нужна Java, команда `keytool` идёт вместе с ней):

   ```bash
   keytool -genkeypair -v -keystore release.jks -keyalg RSA -keysize 2048 -validity 10000 -alias tigersb
   ```

   Запомните пароль. Файл `release.jks` и пароль храните в надёжном месте: если их потерять, обновления перестанут ставиться поверх.

2. Превратите файл в строку:

   ```bash
   base64 -w0 release.jks      # Linux
   certutil -encodehex -f release.jks release.b64 0x40000001   # Windows, строка будет в release.b64
   ```

3. В репозитории откройте **Settings → Secrets and variables → Actions → New repository secret** и добавьте:

   | Имя | Значение |
   |---|---|
   | `ANDROID_KEYSTORE_BASE64` | строка из шага 2 |
   | `ANDROID_KEYSTORE_PASSWORD` | пароль из шага 1 |
   | `ANDROID_KEY_ALIAS` | `tigersb` |
   | `ANDROID_KEY_PASSWORD` | тот же пароль (если при создании не задавали отдельный) |

Со следующей сборки APK будет подписан этим ключом.
