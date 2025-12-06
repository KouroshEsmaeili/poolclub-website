from dataclasses import dataclass, field
from typing import Optional, Dict, List
import datetime as dt

from datetime import datetime
from flask_login import UserMixin
from werkzeug.security import generate_password_hash, check_password_hash


# ---------------------------
# Wallet Transaction Model
# ---------------------------

@dataclass
class WalletTransaction:
    amount: int
    type: str  # deposit, purchase, refund
    timestamp: dt.datetime = field(default_factory=dt.datetime.utcnow)
    description: str = ""


# ---------------------------
# User Model
# ---------------------------

@dataclass
class User(UserMixin):
    id: str
    email: str
    password_hash: str
    first_name: str = ""
    last_name: str = ""

    # WALLET
    wallet_balance: int = 0
    wallet_transactions: List[WalletTransaction] = field(default_factory=list)

    def check_password(self, password: str) -> bool:
        return check_password_hash(self.password_hash, password)

    def deposit(self, amount: int, description: str = "شارژ کیف پول"):
        self.wallet_balance += amount
        self.wallet_transactions.append(
            WalletTransaction(amount=amount, type="deposit", description=description)
        )

    def charge(self, amount: int, description: str = "خرید یا رزرو") -> bool:
        if self.wallet_balance >= amount:
            self.wallet_balance -= amount
            self.wallet_transactions.append(
                WalletTransaction(amount=-amount, type="purchase", description=description)
            )
            return True
        return False

# ---------------------------
# TEMPORARY In-Memory Storage
# ---------------------------

_USERS_BY_ID: Dict[str, User] = {}
_USERS_BY_EMAIL: Dict[str, User] = {}

def create_user(email: str, password: str, first_name: str = "", last_name: str = "") -> User:
    email_norm = email.lower().strip()
    new_id = str(len(_USERS_BY_ID) + 1)

    user = User(
        id=new_id,
        email=email_norm,
        password_hash=generate_password_hash(password),
        first_name=first_name,
        last_name=last_name,
    )

    _USERS_BY_ID[user.id] = user
    _USERS_BY_EMAIL[user.email] = user

    return user


def get_user_by_email(email: str) -> Optional[User]:
    if not email:
        return None
    return _USERS_BY_EMAIL.get(email.lower().strip())


def get_user_by_id(user_id: str) -> Optional[User]:
    return _USERS_BY_ID.get(str(user_id))


# Seed a test user
if not _USERS_BY_EMAIL:
    create_user("test@poolclub.ir", "123456", first_name="کاربر", last_name="آزمایشی")


# ---------------------------
# Booking Model
# ---------------------------
@dataclass
class Booking:
    id: str
    user_id: str
    date: str
    time: str
    duration: int
    type: str  # مثل: "آزاد", "لاین تمرین", "کلاس"
    lane: Optional[int] = None
    status: str = "active"  # active, cancelled, expired



# ---------------------------
# TEMPORARY In-Memory Storage
# ---------------------------
_BOOKINGS: Dict[str, Booking] = {}
_BOOKING_COUNTER = 1
POOL_MAX_CAPACITY = 40

def create_booking(user_id, date, time, duration, booking_type, lane=None):
    global _BOOKING_COUNTER

    booking_id = str(_BOOKING_COUNTER)
    _BOOKING_COUNTER += 1

    booking = Booking(
        id=booking_id,
        user_id=user_id,
        date=date,
        time=time,
        duration=duration,
        type=booking_type,
        lane=lane
    )

    _BOOKINGS[booking_id] = booking
    return booking


def get_user_bookings(user_id):
    return [b for b in _BOOKINGS.values() if b.user_id == user_id]


def cancel_booking(booking_id):
    if booking_id in _BOOKINGS:
        _BOOKINGS[booking_id].status = "cancelled"
        return True
    return False

def parse_datetime(date, time):
    """Convert date + time string into a Python datetime."""
    try:
        return dt.datetime.strptime(f"{date} {time}", "%Y-%m-%d %H:%M")
    except:
        return None

def is_past_booking(date, time):
    booking_dt = parse_datetime(date, time)
    if not booking_dt:
        return True
    return booking_dt < dt.datetime.now()


def user_has_overlap(user_id, date, time, duration):
    """Check if user already has a booking overlapping this one."""
    new_start = parse_datetime(date, time)
    new_end = new_start + dt.timedelta(minutes=duration)

    for b in _BOOKINGS.values():
        if b.user_id != user_id or b.status != "active":
            continue

        existing_start = parse_datetime(b.date, b.time)
        existing_end = existing_start + dt.timedelta(minutes=b.duration)

        # Overlap check
        if (new_start < existing_end) and (existing_start < new_end):
            return True

    return False

AVAILABLE_LANES = [1, 2, 3, 4, 5, 6]

def assign_lane(date, time, duration, booking_type):
    if booking_type != "لاین تمرین":
        return None  # no lane needed
    
    new_start = parse_datetime(date, time)
    new_end = new_start + dt.timedelta(minutes=duration)

    for lane in AVAILABLE_LANES:
        lane_is_free = True

        for b in _BOOKINGS.values():
            if b.status != "active" or b.lane != lane:
                continue

            existing_start = parse_datetime(b.date, b.time)
            existing_end = existing_start + dt.timedelta(minutes=b.duration)

            if (new_start < existing_end) and (existing_start < new_end):
                lane_is_free = False
                break

        if lane_is_free:
            return lane

    return None


def get_next_reservation(user_id):
    future = []
    now = dt.datetime.now()

    for b in _BOOKINGS.values():
        if b.user_id != user_id or b.status != "active":
            continue
        start_dt = parse_datetime(b.date, b.time)
        if start_dt and start_dt > now:
            future.append((start_dt, b))

    if not future:
        return None

    # Return booking with earliest date/time
    future.sort(key=lambda x: x[0])
    return future[0][1]

def count_pool_swimmers(date, time, duration):
    """Count users with free-swim booking overlapping this interval."""
    new_start = parse_datetime(date, time)
    new_end = new_start + dt.timedelta(minutes=duration)

    count = 0

    for b in _BOOKINGS.values():
        if b.type != "شنای آزاد" or b.status != "active":
            continue

        existing_start = parse_datetime(b.date, b.time)
        existing_end = existing_start + dt.timedelta(minutes=b.duration)

        # Check overlap
        if (new_start < existing_end) and (existing_start < new_end):
            count += 1

    return count



def refresh_booking_statuses():
    """به‌روزرسانی وضعیت رزروها بر اساس تاریخ/ساعت فعلی.

    - رزروهای active که تاریخ/ساعت‌شان گذشته است → expired
    - رزروهای cancelled همان‌طور می‌مانند
    """
    now = datetime.now()
    for booking in _BOOKINGS.values():
        # فقط رزروهای فعال را چک می‌کنیم
        if booking.status == "active" and is_past_booking(booking.date, booking.time):
            booking.status = "expired"