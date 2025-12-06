from flask import Blueprint, render_template, current_app, abort, request, jsonify
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
)


main = Blueprint('main', __name__)



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
    facilities = load_json('facilities.json')
    classes = load_json('classes.json')

    raw_events = [e for e in load_json('events.json') if e.get('status') == 'published']
    events = sorted(raw_events, key=parse_date)
    
    ratings = load_json('ratings.json')  
    prices = load_json('prices.json')

    return render_template('index.html', site=site,
                                         hours=hours,
                                         facilities=facilities,
                                         classes=classes,
                                         events=events,
                                         ratings=ratings,
                                         prices=prices)


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

    # فعلاً کلاس‌ها را خالی می‌گذاریم تا بعداً ماژول کلاس‌ها را اضافه کنیم
    my_classes = []

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

# Later when we implement /dashboard/bookings, we can fetch:
# user_bookings = get_user_bookings(current_user.id)
# And show:
# Active bookings
# Cancelled bookings
# Next booking (soonest upcoming)


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
