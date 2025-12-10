from flask import Blueprint, render_template, current_app, abort, request, jsonify, redirect, url_for, flash
from pathlib import Path
import json
import datetime as dt

from flask_login import login_required, current_user
from .model import (
    create_booking,
    get_user_bookings,
    cancel_booking,
    is_past_booking,
    user_has_overlap,
    count_pool_swimmers,
    assign_lane,
    POOL_MAX_CAPACITY,
    get_next_reservation,
    refresh_booking_statuses,
    activate_membership,
    cancel_membership,
    enroll_in_class,
    register_for_event,
    count_event_registrations,
    get_user_event_registrations,
    count_event_registrations,
    user_is_registered_for_event,
    get_user_event_registrations,
    create_event_registration,
)
from .swimcloud_scraper import fetch_swimcloud_rankings


main = Blueprint('main', __name__)


def find_class_by_slug(slug: str):
    """
    کلاس را از فایل classes.json بر اساس slug برمی‌گرداند.
    خروجی: (category_key, class_dict) یا (None, None)
    """
    classes_cfg = load_json("classes.json")
    for cat in classes_cfg.get("categories", []):
        for c in cat.get("items", []):
            if c.get("slug") == slug:
                return cat.get("key"), c
    return None, None

def parse_date(e):
    d = e.get('date')
    try:
        return dt.datetime.fromisoformat(d) if d else dt.datetime.max
    except ValueError:
        return dt.datetime.max


def load_json(name: str):
    data_dir = Path(current_app.config['DATA_DIR'])
    fp = data_dir / name
    try:
        with open(fp, 'r', encoding='utf-8-sig') as f:
            return json.load(f)
    except FileNotFoundError:
        abort(500, description=f"JSON file not found: {fp}")
    except json.JSONDecodeError as e:
        abort(500, description=f"Invalid JSON in {fp} at line {e.lineno}, col {e.colno}: {e.msg}")

@main.route('/')
def index():
    site = load_json('site.json')
    hours = load_json('hours.json')
    pools = load_json('pools.json')           
    programmes = load_json('programmes.json')  
    classes = load_json('classes.json')

    raw_events = [e for e in load_json('events.json') if e.get('status') == 'published']
    events = sorted(raw_events, key=parse_date)

    for e in events:
        slug = e.get("slug")
        if slug:
            e["registered_count"] = count_event_registrations(slug)
            if current_user.is_authenticated:
                e["user_registered"] = user_is_registered_for_event(current_user.id, slug)

    ratings = load_json('ratings.json')  
    prices = load_json('prices.json')

    try:
        live_rankings_men, live_rankings_women, live_rankings_updated_at = fetch_swimcloud_rankings()
    except Exception as exc:
        live_rankings_men = []
        live_rankings_women = []
        live_rankings_updated_at = None
        print("Error fetching Swimcloud rankings:", exc)

    return render_template('index.html', site=site,
                                         hours=hours,
                                         pools=pools,                    
                                         programmes=programmes,                                           
                                         classes=classes,
                                         events=events,
                                         ratings=ratings,
                                         prices=prices,
                                         live_rankings_men=live_rankings_men,
                                         live_rankings_women=live_rankings_women,
                                         live_rankings_updated_at=live_rankings_updated_at,
                                         )



@main.route('/dashboard')
@login_required
def user_dashboard():
    site = load_json('site.json')
    user = current_user
    prices = load_json('prices.json')   
    
    
    refresh_booking_statuses()
    
    # همه رزروهای این کاربر
    user_bookings = get_user_bookings(user.id)

    # محاسبه رزرو بعدی
    next_reservation = get_next_reservation(user.id)

    # تعداد رزروهای آینده و گذشته برای نمایش آمار ساده
    upcoming_count = 0
    past_count = 0

    for b in user_bookings:
        if is_past_booking(b.date, b.time):
            past_count += 1
        else:
            upcoming_count += 1
    
    my_classes = current_user.class_enrollments

    return render_template(
        "user/dashboard.html",
        site=site,
        prices=prices, 
        next_reservation=next_reservation,
        upcoming_bookings_count=upcoming_count,
        past_bookings_count=past_count,
        my_classes=my_classes,
    )

@main.route("/dashboard/wallet")
@login_required
def wallet():
    site = load_json("site.json")
    user = current_user
    transactions = user.wallet_transactions[::-1]  # newest first
    return render_template(
        "user/wallet.html",
        site=site,
        user=user,
        transactions=transactions
    )

@main.route("/dashboard/membership")
@login_required
def membership():
    site = load_json("site.json")
    user = current_user

    memberships_cfg = load_json("memberships.json")
    plans = memberships_cfg.get("plans", [])

    # به‌روزرسانی وضعیت expired
    today = dt.date.today()
    for item in user.membership_history:
        if item.status == "active" and item.expires_at < today:
            item.status = "expired"

    membership_history = sorted(
        user.membership_history,
        key=lambda item: item.purchased_at,
        reverse=True,
    )

    return render_template(
        "user/membership.html",
        site=site,
        user=user,
        plans=plans,
        membership_history=membership_history,
        today=today,
    )

@main.route("/dashboard/membership/buy", methods=["POST"])
@login_required
def membership_buy():
    slug = (request.form.get("plan_slug") or "").strip()

    memberships_cfg = load_json("memberships.json")
    plans = memberships_cfg.get("plans", [])

    plan = next((p for p in plans if p.get("slug") == slug), None)
    if not plan:
        flash("طرح اشتراک انتخاب‌شده یافت نشد.", "danger")
        return redirect(url_for("main.membership"))

    try:
        price = int(plan.get("price", 0) or 0)
        duration_days = int(plan.get("duration_days", 0) or 0)
    except (TypeError, ValueError):
        flash("طرح اشتراک نامعتبر است.", "danger")
        return redirect(url_for("main.membership"))

    if price <= 0 or duration_days <= 0:
        flash("طرح اشتراک نامعتبر است.", "danger")
        return redirect(url_for("main.membership"))

        # کم کردن از کیف پول کاربر
    description = f"خرید اشتراک {plan.get('name', '')}"
    if not current_user.charge(price, description=description):
        flash("موجودی کیف پول برای خرید این اشتراک کافی نیست.", "danger")
        return redirect(url_for("main.membership"))

    # فعال‌سازی / تمدید اشتراک و ثبت در history
    history_item = activate_membership(
        current_user,
        plan_slug=plan["slug"],
        plan_name=plan.get("name", ""),
        duration_days=duration_days,
        price=price,
    )

    flash(
        f"اشتراک «{plan.get('name', '')}» با موفقیت فعال شد. "
        f"اعتبار تا {history_item.expires_at}.",
        "success",
    )

    return redirect(url_for("main.membership"))


@main.route("/dashboard/membership/cancel", methods=["POST"])
@login_required
def membership_cancel():
    history_id = (request.form.get("history_id") or "").strip()
    if not history_id:
        flash("شناسه اشتراک ارسال نشده است.", "danger")
        return redirect(url_for("main.membership"))

    ok, msg, item = cancel_membership(current_user, history_id)
    if not ok:
        flash(msg, "danger")
        return redirect(url_for("main.membership"))

    # بازگشت مبلغ به کیف پول
    refund_amount = item.amount or 0
    if refund_amount > 0:
        current_user.deposit(
            refund_amount,
            description=f"استرداد اشتراک {item.plan_name}",
        )

    flash("اشتراک با موفقیت لغو شد و مبلغ به کیف پول بازگشت.", "success")
    return redirect(url_for("main.membership"))

@main.route("/dashboard/events")
@login_required
def user_events():
    site = load_json("site.json")

    raw_events = [e for e in load_json('events.json') if e.get('status') == 'published']
    events = sorted(raw_events, key=parse_date)

    # annotate events with counts + user registration info
    for e in events:
        slug = e.get("slug")
        if not slug:
            continue
        e["registered_count"] = count_event_registrations(slug)
        e["user_registered"] = user_is_registered_for_event(current_user.id, slug)

    my_regs = get_user_event_registrations(current_user.id)

    return render_template(
        "user/events.html",   # new template
        site=site,
        events=events,
        registrations=my_regs,
    )


@main.route("/api/wallet/deposit", methods=["POST"])
@login_required
def api_wallet_deposit():
    data = request.get_json(silent=True) or {}
    try:
        amount = int(data.get("amount", 0))
    except (TypeError, ValueError):
        return jsonify({"status": "error", "message": "مبلغ نامعتبر است."}), 400

    if amount <= 0:
        return jsonify({"status": "error", "message": "مبلغ نامعتبر است."}), 400

    # Increase user's balance
    current_user.deposit(amount, description="شارژ دستی کیف پول")

    return jsonify({
        "status": "success",
        "message": "کیف پول با موفقیت شارژ شد.",
        "new_balance": current_user.wallet_balance
    })


@main.route("/api/bookings/create", methods=["POST"])
@login_required
def api_create_booking():
    data = request.get_json(silent=True) or {}

    date = data.get("date")
    time = data.get("time")
    duration = data.get("duration")
    booking_type = (data.get("type") or "").strip()

    # نرمال‌سازی متن نوع رزرو برای لاین تمرین
    if booking_type == "رزرو لاین تمرین":
        booking_type = "لاین تمرین"

    # اعتبارسنجی اولیه
    if not date or not time or not duration or not booking_type:
        return jsonify(
            {
                "status": "error",
                "message": "لطفاً تمام فیلدهای مورد نیاز (تاریخ، ساعت، مدت و نوع رزرو) را پر کنید.",
            }
        ), 400

    try:
        duration = int(duration)
        if duration <= 0:
            raise ValueError
    except (TypeError, ValueError):
        return jsonify(
            {"status": "error", "message": "مدت سانس نامعتبر است."}
        ), 400

    # رزرو در گذشته؟
    if is_past_booking(date, time):
        return jsonify(
            {"status": "error", "message": "امکان ثبت رزرو برای زمان گذشته وجود ندارد."}
        ), 400

    # تداخل با رزروهای قبلی کاربر؟
    if user_has_overlap(current_user.id, date, time, duration):
        return jsonify(
            {
                "status": "error",
                "message": "شما در این بازه زمانی رزرو دیگری دارید.",
            }
        ), 409

    # بررسی ظرفیت و لاین‌ها قبل از کم کردن از کیف پول
    lane = None

    # شنای آزاد → محدودیت ظرفیت کلی استخر
    if booking_type == "شنای آزاد":
        swimmers_count = count_pool_swimmers(date, time, duration)
        if swimmers_count >= POOL_MAX_CAPACITY:
            return jsonify(
                {
                    "status": "error",
                    "message": "ظرفیت استخر برای این بازه زمانی تکمیل است.",
                }
            ), 409

    # لاین تمرین → اختصاص خودکار لاین
    elif booking_type == "لاین تمرین":
        lane = assign_lane(date, time, duration, booking_type)
        if lane is None:
            return jsonify(
                {
                    "status": "error",
                    "message": "تمام لاین‌های تمرینی در این بازه زمانی پر هستند.",
                }
            ), 409

    # سایر انواع (اگر بعداً اضافه شوند) فعلاً بدون منطق خاص
    # قیمت‌گذاری بر اساس فایل data/prices.json
    try:
        prices_cfg = load_json("prices.json")
    except Exception:
        # اگر فایل خراب یا در دسترس نباشد، از مقادیر پیش‌فرض استفاده می‌کنیم
        prices_cfg = {}

    default_free_swim = 40000
    default_lane_training = 80000

    if booking_type == "شنای آزاد":
        price = int(prices_cfg.get("free_swim", default_free_swim))
    elif booking_type == "لاین تمرین":
        price = int(prices_cfg.get("lane_training", default_lane_training))
    else:
        # نوع ناشناخته → از شنای آزاد به عنوان پایه استفاده می‌کنیم
        price = int(prices_cfg.get("free_swim", default_free_swim))

    # کم کردن از کیف پول کاربر
    description = f"رزرو سانس ({booking_type})"
    if not current_user.charge(price, description=description):
        return jsonify(
            {
                "status": "error",
                "message": "موجودی کیف پول برای این رزرو کافی نیست.",
            }
        ), 402

    # ایجاد رزرو
    booking = create_booking(
        user_id=current_user.id,
        date=date,
        time=time,
        duration=duration,
        booking_type=booking_type,
        lane=lane,
    )

    return jsonify(
        {
            "status": "success",
            "message": "رزرو با موفقیت ثبت شد.",
            "booking_id": booking.id,
            "lane": lane,
            "new_balance": current_user.wallet_balance,
        }
    ), 201


@main.route("/api/bookings/cancel", methods=["POST"])
@login_required
def api_booking_cancel():
    data = request.get_json(silent=True) or {}
    booking_id = data.get("booking_id")

    if not booking_id:
        return jsonify({"status": "error", "message": "شناسه رزرو ارسال نشده است."}), 400

    if cancel_booking(booking_id):
        return jsonify({"status": "success", "message": "رزرو لغو شد."})
    else:
        return jsonify({"status": "error", "message": "رزرو یافت نشد."}), 404

@main.route("/dashboard/bookings")
@login_required
def bookings():
    site = load_json("site.json")
    prices = load_json("prices.json")  # NEW
    
    refresh_booking_statuses()

    # همه رزروهای این کاربر
    user_bookings = get_user_bookings(current_user.id)

    # مرتب‌سازی بر اساس تاریخ/ساعت
    def sort_key(b):
        from .model import parse_datetime
        dt_obj = parse_datetime(b.date, b.time)
        return dt_obj or dt.datetime.max

    sorted_bookings = sorted(user_bookings, key=sort_key, reverse=True)

    upcoming = []
    past = []

    for b in sorted_bookings:
        if is_past_booking(b.date, b.time):
            past.append(b)
        else:
            upcoming.append(b)


    return render_template(
        "user/bookings.html",
        prices=prices,                 
        site=site,
        upcoming_bookings=upcoming,
        past_bookings=past
    )





@main.route("/api/live-rankings")
def api_live_rankings():
    try:
        items, updated_at = fetch_swimcloud_rankings()
        return jsonify({
            "status": "success",
            "updated_at": updated_at,
            "items": items,
        })
    except Exception as exc:
        print("Error fetching Swimcloud rankings (API):", exc)
        return jsonify({
            "status": "error",
            "message": "خطا در دریافت رده‌بندی زنده.",
        }), 500
    

@main.route("/api/pools")
def api_pools():
    pools = load_json("pools.json")
    return jsonify(pools)


@main.route("/api/programmes")
def api_programmes():
    programmes = load_json("programmes.json")
    return jsonify(programmes)


@main.route("/api/classes/enroll", methods=["POST"])
@login_required
def api_classes_enroll():
    data = request.get_json(silent=True) or {}
    class_slug = (data.get("class_slug") or "").strip()

    if not class_slug:
        return jsonify({"status": "error", "message": "کلاس نامعتبر است."}), 400

    cat_key, class_cfg = find_class_by_slug(class_slug)
    if not class_cfg:
        return jsonify({"status": "error", "message": "کلاس مورد نظر یافت نشد."}), 404

    name = class_cfg.get("name", "")
    coach = class_cfg.get("coach", "")
    time_str = class_cfg.get("time", "")
    price_amount = class_cfg.get("price_amount", 0) or 0

    try:
        price_amount = int(price_amount)
    except (TypeError, ValueError):
        return jsonify({"status": "error", "message": "قیمت کلاس نامعتبر است."}), 400

    if price_amount <= 0:
        return jsonify({"status": "error", "message": "قیمت کلاس نامعتبر است."}), 400

    # Charge wallet
    desc = f"ثبت‌نام در کلاس: {name}"
    if not current_user.charge(price_amount, description=desc):
        return jsonify({
            "status": "error",
            "message": "موجودی کیف پول برای ثبت‌نام در این کلاس کافی نیست."
        }), 402

    # Enroll
    enrollment = enroll_in_class(
        current_user,
        class_slug=class_slug,
        class_name=name,
        coach=coach,
        time=time_str,
        price=price_amount,
    )

    return jsonify({
        "status": "success",
        "message": f"ثبت‌نام در کلاس «{name}» با موفقیت انجام شد.",
        "enrollment_id": enrollment.id,
        "new_balance": current_user.wallet_balance,
    }), 201

@main.route("/dashboard/classes")
@login_required
def user_classes():
    site = load_json("site.json")
    user = current_user

    # همه کلاس‌های تعریف شده در JSON
    classes_cfg = load_json("classes.json")

    # کلاس‌های ثبت‌نام‌شده‌ی این کاربر
    my_classes = getattr(user, "class_enrollments", [])

    return render_template(
        "user/classes.html",
        site=site,
        user=user,
        classes=classes_cfg,     
        my_classes=my_classes,
    )

@main.route("/api/events/register", methods=["POST"])
def api_events_register():
    data = request.get_json(silent=True) or {}
    event_slug = (data.get("event_slug") or "").strip()
    name = (data.get("name") or "").strip()
    email = (data.get("email") or "").strip()

    if not event_slug:
        return jsonify({"status": "error", "message": "رویداد نامعتبر است."}), 400

    event_cfg = find_event_by_slug(event_slug)
    if not event_cfg:
        return jsonify({"status": "error", "message": "رویداد یافت نشد."}), 404

    # اگر کاربر لاگین است، نام و ایمیل را از پروفایل می‌گیریم (در صورت نبود در body)
    if current_user.is_authenticated:
        if not name:
            name = (current_user.first_name or "") + " " + (current_user.last_name or "")
        if not email:
            email = current_user.email

    # برای مهمان‌ها: حداقل ایمیل و نام لازم است
    if not name or not email:
        return jsonify({
            "status": "error",
            "message": "لطفاً نام و ایمیل خود را وارد کنید."
        }), 400

    title = event_cfg.get("title", "")
    user_id = current_user.id if current_user.is_authenticated else None

    # در این فاز، پرداخت واقعی انجام نمی‌دهیم؛ فقط ثبت‌نام را ذخیره می‌کنیم
    reg = register_for_event(
        event_slug=event_slug,
        event_title=title,
        user_id=user_id,
        name=name,
        email=email,
    )

    return jsonify({
        "status": "success",
        "message": f"ثبت‌نام شما برای رویداد «{title}» با موفقیت ثبت شد.",
        "registration_id": reg.id,
    }), 201


def _parse_price_to_int(price_str: str | None) -> int:
    """
    Very simple parser: keeps digits only, e.g. '150,000 تومان' -> 150000.
    Returns 0 if empty or invalid.
    """
    if not price_str:
        return 0
    digits = re.sub(r"[^\d]", "", str(price_str))
    try:
        return int(digits) if digits else 0
    except ValueError:
        return 0


@main.route("/api/events/register", methods=["POST"])
@login_required
def api_event_register():
    data = request.get_json(silent=True) or {}
    slug = (data.get("slug") or "").strip()

    if not slug:
        return jsonify({"status": "error", "message": "شناسه رویداد ارسال نشده است."}), 400

    raw_events = [e for e in load_json('events.json') if e.get('status') == 'published']
    event = next((e for e in raw_events if e.get("slug") == slug), None)
    if not event:
        return jsonify({"status": "error", "message": "رویداد یافت نشد."}), 404

    if event.get("state") != "open":
        return jsonify({"status": "error", "message": "ثبت‌نام این رویداد فعال نیست."}), 409

    # ظرفیت (اختیاری)
    capacity = event.get("capacity")
    current_count = count_event_registrations(slug)
    if capacity is not None:
        try:
            capacity_int = int(capacity)
            if current_count >= capacity_int:
                return jsonify({
                    "status": "error",
                    "message": "ظرفیت این رویداد تکمیل شده است."
                }), 409
        except (TypeError, ValueError):
            pass  # اگر capacity خراب بود، نادیده می‌گیریم

    # کاربر قبلاً ثبت‌نام کرده؟
    if user_is_registered_for_event(current_user.id, slug):
        return jsonify({
            "status": "error",
            "message": "شما قبلاً در این رویداد ثبت‌نام کرده‌اید."
        }), 409

    # مبلغ
    price_str = event.get("price")
    amount = _parse_price_to_int(price_str)
    title = event.get("title") or "رویداد"

    # اگر پولی است، از کیف پول کم کن
    if amount > 0:
        if not current_user.charge(amount, description=f"ثبت‌نام رویداد: {title}"):
            return jsonify({
                "status": "error",
                "message": "موجودی کیف پول کافی نیست."
            }), 402

    # ثبت در حافظه
    reg = create_event_registration(
        user_id=current_user.id,
        event_slug=slug,
        title=title,
        price=amount,
    )

    new_count = count_event_registrations(slug)

    return jsonify({
        "status": "success",
        "message": "ثبت‌نام در رویداد با موفقیت انجام شد.",
        "registration_id": reg.id,
        "new_balance": current_user.wallet_balance,
        "registered_count": new_count,
    })
