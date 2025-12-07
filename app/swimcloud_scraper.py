import requests
from bs4 import BeautifulSoup
from datetime import datetime, timezone

SWIMCLOUD_REGION_URL = "https://www.swimcloud.com/?r=country_USA"


def fetch_swimcloud_rankings(max_rows_per_gender: int = 5):
    """
    Top Swims صفحه‌ی کشور USA در Swimcloud را می‌خواند و دو لیست جداگانه
    برای مردان و زنان برمی‌گرداند.

    خروجی: (men_items, women_items, last_updated_iso)

      men_items / women_items: لیست دیکشنری مثل:
        {
          "rank": "1",
          "name": "Luka Mijatovic",
          "club": "Pleasanton Seahawks",
          "event": "400 Free",
          "time": "3:45.30",
          "score": "932"
        }

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

    section = soup.select_one("section#js-region-top-swims-container")
    if not section:
        now_iso = datetime.now(timezone.utc).isoformat()
        return [], [], now_iso

    men_items = []
    women_items = []

    # دو کارت col-sm-6 در داخل js-top-swims-form-content
    cards = section.select("div.js-top-swims-form-content > div.col-sm-6")

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

            # ستون‌ها مطابق HTML:
            # 0: rank
            # 1: name (+ club in mobile)
            # 2: team/club (logo & title)
            # 3: event (400 Free, 50 Back, ...)
            # 4: time
            # 5: FINA points

            rank_text = tds[0].get_text(strip=True) or str(i)

            # Name
            name_link = tds[1].find("a")
            name_text = name_link.get_text(strip=True) if name_link else tds[1].get_text(strip=True)

            # Club
            club_text = ""
            club_mobile = tds[1].select_one("div.u-color-mute")
            if club_mobile:
                club_text = club_mobile.get_text(strip=True)

            if not club_text and len(tds) > 2:
                team_link = tds[2].find("a")
                if team_link and team_link.has_attr("title"):
                    club_text = team_link["title"].strip()

                if not club_text:
                    img = tds[2].find("img")
                    if img and img.has_attr("alt"):
                        club_text = img["alt"].replace(" logo", "").strip()

            event_text = tds[3].get_text(strip=True)
            time_text = tds[4].get_text(strip=True)
            score_text = tds[5].get_text(strip=True) if len(tds) > 5 else ""

            if not name_text:
                continue

            item = {
                "rank": rank_text,
                "name": name_text,
                "club": club_text,
                "event": event_text,
                "time": time_text,
                "score": score_text,
            }

            if gender.lower().startswith("men"):
                men_items.append(item)
            elif gender.lower().startswith("women"):
                women_items.append(item)
            else:
                # اگر به هر دلیلی gender ناشناخته بود، می‌توانی آن را نادیده بگیری
                continue

    last_updated_iso = datetime.now(timezone.utc).isoformat()
    return men_items, women_items, last_updated_iso
