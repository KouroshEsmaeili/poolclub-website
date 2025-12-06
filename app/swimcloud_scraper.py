import requests
from bs4 import BeautifulSoup
from datetime import datetime, timezone

SWIMCLOUD_REGION_URL = "https://www.swimcloud.com/?r=country_USA"


def fetch_swimcloud_rankings(max_rows_per_gender: int = 5):
    """
    Top Swims صفحه‌ی کشور USA در Swimcloud را می‌خواند و یک لیست رده‌بندی برمی‌گرداند.

    ما از سکشن:
      <section class="c-section" id="js-region-top-swims-container"> ... </section>

    دو جدول را می‌خوانیم: Men و Women.
    هیچ ذخیره‌سازی دیتابیسی انجام نمی‌شود؛ هر بار که این تابع صدا زده شود
    اطلاعات تازه از وب‌سایت خوانده می‌شود.

    خروجی: (items, last_updated_iso)
      items: لیست دیکشنری مثل:
        [
          {
            "rank": "1",
            "name": "Luka Mijatovic",
            "club": "Pleasanton Seahawks",
            "age_group": "Men",          # این‌جا gender را می‌گذاریم
            "stroke": "400 Free",        # در واقع Event از جدول
            "score": "932"               # FINA points
          },
          ...
        ]
      last_updated_iso: رشته‌ی زمان (UTC, ISO 8601) برای نمایش در UI
    """

    headers = {
        "User-Agent": (
            "Mozilla/5.0 (compatible; PoolClubBot/1.0; "
            "+https://yourdomain.example)"
        )
    }

    resp = requests.get(SWIMCLOUD_REGION_URL, headers=headers, timeout=10)
    resp.raise_for_status()

    soup = BeautifulSoup(resp.text, "html.parser")

    # سکشن Top swims
    section = soup.select_one("section#js-region-top-swims-container")
    if not section:
        return [], datetime.now(timezone.utc).isoformat()

    # داخل این سکشن، دو کارت col-sm-6 وجود دارد (Men و Women)
    cards = section.select("div.js-top-swims-form-content > div.col-sm-6")
    items = []

    for card in cards:
        # عنوان جنسیت (Men / Women)
        gender_el = card.select_one("h3.c-title")
        gender = gender_el.get_text(strip=True) if gender_el else ""

        table = card.select_one("table.c-table-clean")
        if not table:
            continue

        tbody = table.find("tbody") or table
        rows = tbody.find_all("tr")

        for i, tr in enumerate(rows, start=1):
            if max_rows_per_gender and i > max_rows_per_gender:
                break

            tds = tr.find_all("td")
            if len(tds) < 5:
                continue

            # ساختار tdها بر اساس HTMLی که دادی:
            # tds[0]: rank number (1, 2, 3, ...)
            # tds[1]: name + (در موبایل) club داخل div.u-color-mute
            # tds[2]: تیم (لوگو و لینک) - نام کامل در title یا alt
            # tds[3]: event (e.g., "400 Free")
            # tds[4]: time (e.g., "3:45.30") with <a>
            # tds[5]: FINA points (e.g., "932")

            rank_text = tds[0].get_text(strip=True) or str(i)

            # Name
            name_link = tds[1].find("a")
            name_text = name_link.get_text(strip=True) if name_link else tds[1].get_text(strip=True)

            # Club – چند روش برای استخراج:
            club_text = ""

            # 1) موبایل: div.u-color-mute inside tds[1]
            club_mobile = tds[1].select_one("div.u-color-mute")
            if club_mobile:
                club_text = club_mobile.get_text(strip=True)

            # 2) دسکتاپ: title روی لینک داخل tds[2]
            if not club_text and len(tds) > 2:
                team_link = tds[2].find("a")
                if team_link and team_link.has_attr("title"):
                    club_text = team_link["title"].strip()

                # 3) اگر باز هم خالی بود، از alt لوگو استفاده کن
                if not club_text:
                    img = tds[2].find("img")
                    if img and img.has_attr("alt"):
                        club_text = img["alt"].replace(" logo", "").strip()

            # Event (ما از این برای ستون "استایل تخصصی" استفاده می‌کنیم)
            event_text = tds[3].get_text(strip=True)

            # Time (فعلاً در UI استفاده نمی‌کنیم، اما شاید بعداً بخواهی)
            time_text = tds[4].get_text(strip=True)

            # FINA score
            score_text = ""
            if len(tds) > 5:
                score_text = tds[5].get_text(strip=True)

            # اگر نام خالی بود، این ردیف را نادیده می‌گیریم
            if not name_text:
                continue

            items.append(
                {
                    "rank": rank_text,
                    "name": name_text,
                    "club": club_text,
                    # این‌جا "رده سنی" را عملاً gender نشان می‌دهیم
                    "age_group": gender,          # Men / Women
                    "stroke": event_text,         # در واقع Event
                    "score": score_text,          # FINA points
                    # زمان را اگر خواستی بعداً در UI استفاده کنی
                    "time": time_text,
                }
            )

    last_updated_iso = datetime.now(timezone.utc).isoformat()
    return items, last_updated_iso
