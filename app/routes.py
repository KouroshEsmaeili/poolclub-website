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
    parse_datetime,
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
    return render_template('index.html', site=site, hours=hours,
                           facilities=facilities, classes=classes, events=events, ratings=ratings)


@main.route('/dashboard')
@login_required
def user_dashboard():
    site = load_json('site.json')
    user = current_user

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
def api_booking_create():
    
    data = request.get_json(silent=True) or {}
    
    date = data.get("date")
    time = data.get("time")
    try:
        duration = int(data.get("duration", 0))
    except (TypeError, ValueError):
        return jsonify({"status": "error", "message": "مدت سانس نامعتبر است."}), 400
    
    raw_type = (data.get("type") or "").strip()
    if raw_type == "رزرو لاین تمرین":
        booking_type = "لاین تمرین"
    else:
        booking_type = raw_type

    price = 40000  # placeholder
    
    # 1) Validate inputs
    if not date or not time or not booking_type:
        return jsonify({"status": "error", "message": "اطلاعات کامل نیست."}), 400

    # 2) Prevent booking in the past
    if is_past_booking(date, time):
        return jsonify({"status": "error", "message": "نمی‌توانید سانس گذشته را رزرو کنید."}), 400

    # 3) Prevent overlapping user bookings
    if user_has_overlap(current_user.id, date, time, duration):
        return jsonify({"status": "error", "message": "شما در این بازه زمانی قبلاً رزرو دارید."}), 409

    # 4) Wallet check
    if not current_user.charge(price, description=f"رزرو سانس ({booking_type})"):
        return jsonify({"status": "error", "message": "موجودی کیف پول کافی نیست."}), 402

    

    if booking_type == "شنای آزاد":
        swimmers = count_pool_swimmers(date, time, duration)
        if swimmers >= POOL_MAX_CAPACITY:
            return jsonify({
                "status": "error",
                "message": "ظرفیت این سانس تکمیل شده است."
            }), 409
        lane = None

    elif booking_type == "لاین تمرین":
        lane = assign_lane(date, time, duration, booking_type)
        if lane is None:
            return jsonify({
                "status": "error",
                "message": "تمام لاین‌ها در این بازه زمانی رزرو شده‌اند."
            }), 409

    else:
        lane = None  # for future: classes/events
    booking = create_booking(
        user_id=current_user.id,
        date=date,
        time=time,
        duration=duration,
        booking_type=booking_type,
        lane=lane
    )

    return jsonify({
        "status": "success",
        "message": "رزرو با موفقیت انجام شد.",
        "booking_id": booking.id,
        "new_balance": current_user.wallet_balance,
        "lane": lane
    })


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
        site=site,
        upcoming_bookings=upcoming,
        past_bookings=past
    )
