from dataclasses import dataclass, field
from typing import Optional, Dict, List, Tuple
import datetime as dt
import uuid

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
# Membership Model
# ---------------------------

@dataclass
class MembershipHistoryItem:
    id: str
    plan_slug: str
    plan_name: str
    purchased_at: dt.datetime
    expires_at: dt.date
    amount: int
    status: str = "active"  # active, cancelled, expired


# ---------------------------
# Class Enrollment Model
# ---------------------------

@dataclass
class ClassEnrollment:
    id: str
    class_slug: str
    class_name: str
    coach: str
    time: str
    price: int
    enrolled_at: dt.datetime
    status: str = "active"  # active, cancelled


# ---------------------------
# Event Registration Model
#   (used for both wallet & public registrations)
# ---------------------------

@dataclass
class EventRegistration:
    id: str
    event_slug: str
    title: str
    user_id: Optional[str]  # None for guests
    name: str
    email: str
    price: int = 0
    created_at: dt.datetime = field(default_factory=dt.datetime.utcnow)
    status: str = "registered"  # registered, cancelled


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

    # Wallet
    wallet_balance: int = 0
    wallet_transactions: List[WalletTransaction] = field(default_factory=list)

    # Membership
    membership_slug: Optional[str] = None
    membership_name: Optional[str] = None
    membership_expires_at: Optional[dt.date] = None
    membership_history: List[MembershipHistoryItem] = field(default_factory=list)

    # Profile
    phone: str = ""
    birthdate: str = ""          # e.g. "2000-01-01"
    emergency_contact: str = ""  # text field

    # Classes
    class_enrollments: List[ClassEnrollment] = field(default_factory=list)

    # ---- Methods ----

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
                WalletTransaction(
                    amount=-amount,
                    type="purchase",
                    description=description,
                )
            )
            return True
        return False

    def has_active_membership(self) -> bool:
        today = dt.date.today()
        if not self.membership_slug or not self.membership_expires_at:
            return False
        if self.membership_expires_at < today:
            return False

        for item in reversed(self.membership_history):
            if item.plan_slug == self.membership_slug:
                return item.status == "active" and item.expires_at >= today
        return True

    def clear_membership(self):
        """Remove any membership info (used if you ever implement full cancel)."""
        self.membership_slug = None
        self.membership_name = None
        self.membership_expires_at = None


# ---------------------------
# Booking Model
# ---------------------------

@dataclass
class Booking:
    id: str
    user_id: str
    date: str          # "YYYY-MM-DD"
    time: str          # "HH:MM"
    duration: int      # minutes
    type: str          # e.g. "شنای آزاد", "لاین تمرین"
    lane: Optional[int] = None
    status: str = "active"  # active, cancelled, expired


# ---------------------------
# In-Memory Storage
# ---------------------------

_USERS_BY_ID: Dict[str, User] = {}
_USERS_BY_EMAIL: Dict[str, User] = {}

_BOOKINGS: Dict[str, Booking] = {}
_BOOKING_COUNTER = 1

POOL_MAX_CAPACITY = 40

_EVENT_REGISTRATIONS: List[EventRegistration] = []


# ---------------------------
# User helpers
# ---------------------------

def create_user(
    email: str,
    password: str,
    first_name: str = "",
    last_name: str = "",
) -> User:
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


# Seed a test user (dev only)
if not _USERS_BY_EMAIL:
    create_user("test", "123456", first_name="کاربر", last_name="آزمایشی")


def update_user_email(user: User, new_email: str) -> bool:
    """
    Try to update the user's email and keep the _USERS_BY_EMAIL index in sync.
    Returns True on success, False if email is invalid or already taken.
    """
    global _USERS_BY_EMAIL

    email_norm = new_email.lower().strip()
    if not email_norm:
        return False

    if email_norm == user.email:
        return True

    if email_norm in _USERS_BY_EMAIL:
        return False

    old_email = user.email
    if old_email in _USERS_BY_EMAIL:
        del _USERS_BY_EMAIL[old_email]

    user.email = email_norm
    _USERS_BY_EMAIL[email_norm] = user
    return True


# ---------------------------
# Booking helpers
# ---------------------------

def create_booking(
    user_id: str,
    date: str,
    time: str,
    duration: int,
    booking_type: str,
    lane: Optional[int] = None,
) -> Booking:
    global _BOOKING_COUNTER

    booking_id = str(_BOOKING_COUNTER)
    _BOOKING_COUNTER += 1

    booking = Booking(
        id=booking_id,
        user_id=str(user_id),
        date=date,
        time=time,
        duration=duration,
        type=booking_type,
        lane=lane,
    )
    _BOOKINGS[booking_id] = booking
    return booking


def get_user_bookings(user_id: str) -> List[Booking]:
    return [b for b in _BOOKINGS.values() if b.user_id == str(user_id)]


def cancel_booking(booking_id: str) -> bool:
    if booking_id in _BOOKINGS:
        _BOOKINGS[booking_id].status = "cancelled"
        return True
    return False


def parse_datetime(date: str, time: str) -> Optional[dt.datetime]:
    """Convert date + time string into a Python datetime."""
    try:
        return dt.datetime.strptime(f"{date} {time}", "%Y-%m-%d %H:%M")
    except Exception:
        return None


def is_past_booking(date: str, time: str) -> bool:
    booking_dt = parse_datetime(date, time)
    if not booking_dt:
        return True
    return booking_dt < dt.datetime.now()


def user_has_overlap(user_id: str, date: str, time: str, duration: int) -> bool:
    """Check if user already has a booking overlapping this one."""
    new_start = parse_datetime(date, time)
    if not new_start:
        return False
    new_end = new_start + dt.timedelta(minutes=duration)

    for b in _BOOKINGS.values():
        if b.user_id != str(user_id) or b.status != "active":
            continue

        existing_start = parse_datetime(b.date, b.time)
        if not existing_start:
            continue
        existing_end = existing_start + dt.timedelta(minutes=b.duration)

        if (new_start < existing_end) and (existing_start < new_end):
            return True

    return False


AVAILABLE_LANES = [1, 2, 3, 4, 5, 6]


def assign_lane(date: str, time: str, duration: int, booking_type: str) -> Optional[int]:
    if booking_type != "لاین تمرین":
        return None

    new_start = parse_datetime(date, time)
    if not new_start:
        return None
    new_end = new_start + dt.timedelta(minutes=duration)

    for lane in AVAILABLE_LANES:
        lane_is_free = True

        for b in _BOOKINGS.values():
            if b.status != "active" or b.lane != lane:
                continue

            existing_start = parse_datetime(b.date, b.time)
            if not existing_start:
                continue
            existing_end = existing_start + dt.timedelta(minutes=b.duration)

            if (new_start < existing_end) and (existing_start < new_end):
                lane_is_free = False
                break

        if lane_is_free:
            return lane

    return None


def get_next_reservation(user_id: str) -> Optional[Booking]:
    future: List[Tuple[dt.datetime, Booking]] = []
    now = dt.datetime.now()

    for b in _BOOKINGS.values():
        if b.user_id != str(user_id) or b.status != "active":
            continue
        start_dt = parse_datetime(b.date, b.time)
        if start_dt and start_dt > now:
            future.append((start_dt, b))

    if not future:
        return None

    future.sort(key=lambda x: x[0])
    return future[0][1]


def count_pool_swimmers(date: str, time: str, duration: int) -> int:
    """Count users with free-swim booking overlapping this interval."""
    new_start = parse_datetime(date, time)
    if not new_start:
        return 0
    new_end = new_start + dt.timedelta(minutes=duration)

    count = 0
    for b in _BOOKINGS.values():
        if b.type != "شنای آزاد" or b.status != "active":
            continue

        existing_start = parse_datetime(b.date, b.time)
        if not existing_start:
            continue
        existing_end = existing_start + dt.timedelta(minutes=b.duration)

        if (new_start < existing_end) and (existing_start < new_end):
            count += 1

    return count


def refresh_booking_statuses():
    """Update booking.status based on current time."""
    for booking in _BOOKINGS.values():
        if booking.status == "active" and is_past_booking(booking.date, booking.time):
            booking.status = "expired"


# ---------------------------
# Membership helpers
# ---------------------------

def activate_membership(
    user: User,
    plan_slug: str,
    plan_name: str,
    duration_days: int,
    price: int,
) -> MembershipHistoryItem:
    """
    Activate or extend membership:
      - If same plan is already active → extend from its current expiration.
      - Otherwise → start from today.
    """
    now = dt.datetime.now()
    today = now.date()

    if (
        user.membership_slug == plan_slug
        and user.membership_expires_at
        and user.membership_expires_at >= today
    ):
        start_date = user.membership_expires_at
    else:
        start_date = today

    expires_at = start_date + dt.timedelta(days=duration_days)

    user.membership_slug = plan_slug
    user.membership_name = plan_name
    user.membership_expires_at = expires_at

    # Auto mark old active items as expired
    for item in user.membership_history:
        if item.status == "active" and item.expires_at < today:
            item.status = "expired"

    history_item = MembershipHistoryItem(
        id=str(uuid.uuid4()),
        plan_slug=plan_slug,
        plan_name=plan_name,
        purchased_at=now,
        expires_at=expires_at,
        amount=price,
        status="active",
    )
    user.membership_history.append(history_item)
    return history_item


def cancel_membership(
    user: User,
    history_id: str,
) -> Tuple[bool, str, Optional[MembershipHistoryItem]]:
    """
    Cancel membership:
      - Only if item is active and purchased today.
      - Marks status as cancelled.
      - Clears user's current membership if it matches this record.
    """
    today = dt.date.today()

    for item in user.membership_history:
        if item.id == history_id:
            if item.status != "active":
                return False, "این اشتراک در حال حاضر فعال نیست.", None
            if item.purchased_at.date() != today:
                return False, "امکان لغو اشتراک فقط در روز خرید وجود دارد.", None

            item.status = "cancelled"

            if (
                user.membership_slug == item.plan_slug
                and user.membership_expires_at == item.expires_at
            ):
                user.clear_membership()

            return True, "", item

    return False, "اشتراک مورد نظر یافت نشد.", None


# ---------------------------
# Class helpers
# ---------------------------

def enroll_in_class(
    user: User,
    class_slug: str,
    class_name: str,
    coach: str,
    time: str,
    price: int,
) -> ClassEnrollment:
    now = dt.datetime.now()
    enrollment = ClassEnrollment(
        id=str(uuid.uuid4()),
        class_slug=class_slug,
        class_name=class_name,
        coach=coach,
        time=time,
        price=price,
        enrolled_at=now,
        status="active",
    )
    user.class_enrollments.append(enrollment)
    return enrollment


# ---------------------------
# Event helpers (public + wallet)
# ---------------------------

def register_for_event(
    event_slug: str,
    event_title: str,
    user_id: Optional[str],
    name: str,
    email: str,
) -> EventRegistration:
    """
    Public/guest registration (or logged-in without wallet payment).
    Price is assumed 0 here.
    """
    # If user_id given but name/email empty, try to pull from user object
    if user_id and (not name or not email):
        u = get_user_by_id(user_id)
        if u:
            if not name:
                name = (u.first_name or "") + " " + (u.last_name or "")
            if not email:
                email = u.email

    reg = EventRegistration(
        id=str(uuid.uuid4()),
        event_slug=event_slug,
        title=event_title,
        user_id=str(user_id) if user_id is not None else None,
        name=name,
        email=email,
        price=0,
        status="registered",
    )
    _EVENT_REGISTRATIONS.append(reg)
    return reg


def create_event_registration(
    user_id: str,
    event_slug: str,
    title: str,
    price: int,
) -> EventRegistration:
    """
    Wallet-based event registration for authenticated users.
    Name/email are taken from the user profile when available.
    """
    u = get_user_by_id(user_id)
    if u:
        name = (u.first_name or "") + " " + (u.last_name or "")
        email = u.email
    else:
        name = ""
        email = ""

    reg = EventRegistration(
        id=str(uuid.uuid4()),
        event_slug=event_slug,
        title=title,
        user_id=str(user_id),
        name=name,
        email=email,
        price=price,
        status="registered",
    )
    _EVENT_REGISTRATIONS.append(reg)
    return reg


def count_event_registrations(event_slug: str) -> int:
    return sum(
        1
        for r in _EVENT_REGISTRATIONS
        if r.event_slug == event_slug and r.status == "registered"
    )


def get_user_event_registrations(user_id: str) -> List[EventRegistration]:
    return [
        r
        for r in _EVENT_REGISTRATIONS
        if r.user_id == str(user_id)
    ]


def user_is_registered_for_event(user_id: str, event_slug: str) -> bool:
    return any(
        r.user_id == str(user_id)
        and r.event_slug == event_slug
        and r.status == "registered"
        for r in _EVENT_REGISTRATIONS
    )
