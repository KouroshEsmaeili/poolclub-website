from __future__ import annotations

from typing import Optional, List, Tuple
import datetime as dt
import uuid

from flask_login import UserMixin
from werkzeug.security import generate_password_hash, check_password_hash

from . import db

# ---------------------------
# Constants
# ---------------------------

POOL_MAX_CAPACITY = 40
AVAILABLE_LANES = [1, 2, 3, 4, 5, 6]


# ---------------------------
# ORM Models
# ---------------------------


class User(UserMixin, db.Model):
    __tablename__ = "users"

    id = db.Column(db.Integer, primary_key=True)
    email = db.Column(db.String(255), unique=True, nullable=False, index=True)
    password_hash = db.Column(db.String(255), nullable=False)

    first_name = db.Column(db.String(100), default="")
    last_name = db.Column(db.String(100), default="")

    # Wallet
    wallet_balance = db.Column(db.Integer, nullable=False, default=0)

    # Membership
    membership_slug = db.Column(db.String(100), nullable=True)
    membership_name = db.Column(db.String(200), nullable=True)
    membership_expires_at = db.Column(db.Date, nullable=True)

    # Profile
    phone = db.Column(db.String(50), default="")
    birthdate = db.Column(db.String(20), default="")           # e.g. "2000-01-01"
    emergency_contact = db.Column(db.String(255), default="")  # free text

    # Relationships
    wallet_transactions = db.relationship(
        "WalletTransaction",
        backref="user",
        lazy=True,
        cascade="all, delete-orphan",
    )
    membership_history = db.relationship(
        "MembershipHistoryItem",
        backref="user",
        lazy=True,
        cascade="all, delete-orphan",
    )
    class_enrollments = db.relationship(
        "ClassEnrollment",
        backref="user",
        lazy=True,
        cascade="all, delete-orphan",
    )
    bookings = db.relationship(
        "Booking",
        backref="user",
        lazy=True,
        cascade="all, delete-orphan",
    )
    event_registrations = db.relationship(
        "EventRegistration",
        backref="user",
        lazy=True,
        cascade="all, delete-orphan",
    )

    # ---- Methods ----

    def check_password(self, password: str) -> bool:
        return check_password_hash(self.password_hash, password)

    def deposit(self, amount: int, description: str = "شارژ کیف پول"):
        """Increase wallet balance and record a transaction."""
        self.wallet_balance += amount
        wt = WalletTransaction(
            user_id=self.id,
            amount=amount,
            type="deposit",
            description=description,
        )
        db.session.add(wt)
        db.session.add(self)
        db.session.commit()

    def charge(self, amount: int, description: str = "خرید یا رزرو") -> bool:
        """Charge wallet if balance is enough; return True/False."""
        if self.wallet_balance >= amount:
            self.wallet_balance -= amount
            wt = WalletTransaction(
                user_id=self.id,
                amount=-amount,
                type="purchase",
                description=description,
            )
            db.session.add(wt)
            db.session.add(self)
            db.session.commit()
            return True
        return False

    def has_active_membership(self) -> bool:
        today = dt.date.today()
        if not self.membership_slug or not self.membership_expires_at:
            return False
        if self.membership_expires_at < today:
            return False

        # آخرین رکورد این پلن را چک می‌کنیم
        last_for_plan: Optional[MembershipHistoryItem] = (
            MembershipHistoryItem.query
            .filter_by(user_id=self.id, plan_slug=self.membership_slug)
            .order_by(MembershipHistoryItem.purchased_at.desc())
            .first()
        )
        if not last_for_plan:
            return False
        return last_for_plan.status == "active" and last_for_plan.expires_at >= today

    def clear_membership(self):
        """پاک کردن وضعیت اشتراک فعلی کاربر (برای لغو)."""
        self.membership_slug = None
        self.membership_name = None
        self.membership_expires_at = None
        db.session.add(self)
        db.session.commit()

    def get_id(self) -> str:
        """Flask-Login expects a string id."""
        return str(self.id)


class WalletTransaction(db.Model):
    __tablename__ = "wallet_transactions"

    id = db.Column(db.Integer, primary_key=True)
    user_id = db.Column(db.Integer, db.ForeignKey("users.id"), nullable=False)
    amount = db.Column(db.Integer, nullable=False)
    type = db.Column(db.String(20), nullable=False)  # deposit, purchase, refund
    timestamp = db.Column(db.DateTime, nullable=False, default=dt.datetime.utcnow)
    description = db.Column(db.String(255), default="")


class MembershipHistoryItem(db.Model):
    __tablename__ = "membership_history"

    id = db.Column(db.String(36), primary_key=True)  # UUID
    user_id = db.Column(db.Integer, db.ForeignKey("users.id"), nullable=False)
    plan_slug = db.Column(db.String(100), nullable=False)
    plan_name = db.Column(db.String(200), nullable=False)
    purchased_at = db.Column(db.DateTime, nullable=False)
    expires_at = db.Column(db.Date, nullable=False)
    amount = db.Column(db.Integer, nullable=False)
    status = db.Column(db.String(20), nullable=False, default="active")  # active, cancelled, expired


class ClassEnrollment(db.Model):
    __tablename__ = "class_enrollments"

    id = db.Column(db.String(36), primary_key=True)  # UUID
    user_id = db.Column(db.Integer, db.ForeignKey("users.id"), nullable=False)
    class_slug = db.Column(db.String(100), nullable=False)
    class_name = db.Column(db.String(200), nullable=False)
    coach = db.Column(db.String(200), nullable=True)
    time = db.Column(db.String(100), nullable=True)
    price = db.Column(db.Integer, nullable=False)
    enrolled_at = db.Column(db.DateTime, nullable=False)
    status = db.Column(db.String(20), nullable=False, default="active")  # active, cancelled


class Booking(db.Model):
    __tablename__ = "bookings"

    id = db.Column(db.Integer, primary_key=True)
    user_id = db.Column(db.Integer, db.ForeignKey("users.id"), nullable=False)

    date = db.Column(db.String(10), nullable=False)   # "YYYY-MM-DD"
    time = db.Column(db.String(5), nullable=False)    # "HH:MM"
    duration = db.Column(db.Integer, nullable=False)  # minutes

    type = db.Column(db.String(50), nullable=False)   # e.g. "شنای آزاد", "لاین تمرین"
    lane = db.Column(db.Integer, nullable=True)
    status = db.Column(db.String(20), nullable=False, default="active")  # active, cancelled, expired


class EventRegistration(db.Model):
    """
    Unified event registration model:
      - used for wallet-based and public (guest) registrations.
    """
    __tablename__ = "event_registrations"

    id = db.Column(db.Integer, primary_key=True)
    user_id = db.Column(db.Integer, db.ForeignKey("users.id"), nullable=True)
    event_slug = db.Column(db.String(100), nullable=False)
    title = db.Column(db.String(255), nullable=False)

    # Optional details when user is anonymous or we want explicit info
    name = db.Column(db.String(255), nullable=True)
    email = db.Column(db.String(255), nullable=True)

    price = db.Column(db.Integer, nullable=False, default=0)
    created_at = db.Column(db.DateTime, nullable=False, default=dt.datetime.utcnow)
    status = db.Column(db.String(20), nullable=False, default="registered")  # registered, cancelled


# ---------------------------
# User helpers
# ---------------------------


def create_user(
    email: str,
    password: str,
    first_name: str = "",
    last_name: str = "",
) -> User:
    email_norm = (email or "").lower().strip()
    user = User(
        email=email_norm,
        password_hash=generate_password_hash(password),
        first_name=first_name,
        last_name=last_name,
    )
    db.session.add(user)
    db.session.commit()
    return user


def get_user_by_email(email: str) -> Optional[User]:
    if not email:
        return None
    return User.query.filter_by(email=email.lower().strip()).first()


def get_user_by_id(user_id: str | int) -> Optional[User]:
    try:
        return User.query.get(int(user_id))
    except (TypeError, ValueError):
        return None


def update_user_email(user: User, new_email: str) -> bool:
    """
    Try to update the user's email and keep uniqueness.
    Returns True on success, False if email is invalid or already taken.
    """
    email_norm = (new_email or "").lower().strip()
    if not email_norm:
        return False

    if email_norm == user.email:
        return True

    # Already taken by another user?
    existing = User.query.filter(
        User.email == email_norm,
        User.id != user.id,
    ).first()
    if existing:
        return False

    user.email = email_norm
    db.session.add(user)
    db.session.commit()
    return True


# ---------------------------
# Booking helpers
# ---------------------------


def create_booking(
    user_id: str | int,
    date: str,
    time: str,
    duration: int,
    booking_type: str,
    lane: Optional[int] = None,
) -> Booking:
    booking = Booking(
        user_id=int(user_id),
        date=date,
        time=time,
        duration=duration,
        type=booking_type,
        lane=lane,
    )
    db.session.add(booking)
    db.session.commit()
    return booking


def get_user_bookings(user_id: str | int) -> List[Booking]:
    return Booking.query.filter_by(user_id=int(user_id)).all()


def cancel_booking(booking_id: str | int) -> bool:
    try:
        b_id = int(booking_id)
    except (TypeError, ValueError):
        return False

    booking = Booking.query.get(b_id)
    if not booking:
        return False
    booking.status = "cancelled"
    db.session.add(booking)
    db.session.commit()
    return True


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


def user_has_overlap(user_id: str | int, date: str, time: str, duration: int) -> bool:
    """Check if user already has a booking overlapping this one."""
    new_start = parse_datetime(date, time)
    if not new_start:
        return False
    new_end = new_start + dt.timedelta(minutes=duration)

    for b in Booking.query.filter_by(user_id=int(user_id), status="active").all():
        existing_start = parse_datetime(b.date, b.time)
        if not existing_start:
            continue
        existing_end = existing_start + dt.timedelta(minutes=b.duration)

        # Overlap check
        if (new_start < existing_end) and (existing_start < new_end):
            return True

    return False


def assign_lane(date: str, time: str, duration: int, booking_type: str) -> Optional[int]:
    if booking_type != "لاین تمرین":
        return None

    new_start = parse_datetime(date, time)
    if not new_start:
        return None
    new_end = new_start + dt.timedelta(minutes=duration)

    for lane in AVAILABLE_LANES:
        lane_is_free = True

        for b in Booking.query.filter_by(status="active", lane=lane).all():
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


def get_next_reservation(user_id: str | int) -> Optional[Booking]:
    future: List[Tuple[dt.datetime, Booking]] = []
    now = dt.datetime.now()

    for b in Booking.query.filter_by(user_id=int(user_id), status="active").all():
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
    for b in Booking.query.filter_by(type="شنای آزاد", status="active").all():
        existing_start = parse_datetime(b.date, b.time)
        if not existing_start:
            continue
        existing_end = existing_start + dt.timedelta(minutes=b.duration)

        # Check overlap
        if (new_start < existing_end) and (existing_start < new_end):
            count += 1

    return count


def refresh_booking_statuses():
    """Update booking.status based on current time."""
    changed = False
    for booking in Booking.query.filter_by(status="active").all():
        if is_past_booking(booking.date, booking.time):
            booking.status = "expired"
            db.session.add(booking)
            changed = True
    if changed:
        db.session.commit()


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
        user_id=user.id,
        plan_slug=plan_slug,
        plan_name=plan_name,
        purchased_at=now,
        expires_at=expires_at,
        amount=price,
        status="active",
    )
    db.session.add(history_item)
    db.session.add(user)
    db.session.commit()
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

    item = MembershipHistoryItem.query.filter_by(
        id=history_id,
        user_id=user.id,
    ).first()
    if not item:
        return False, "اشتراک مورد نظر یافت نشد.", None

    if item.status != "active":
        return False, "این اشتراک در حال حاضر فعال نیست.", None

    if item.purchased_at.date() != today:
        return False, "امکان لغو اشتراک فقط در روز خرید وجود دارد.", None

    item.status = "cancelled"

    if (
        user.membership_slug == item.plan_slug
        and user.membership_expires_at == item.expires_at
    ):
        user.clear_membership()  # این خودش commit می‌کند
    else:
        db.session.add(item)
        db.session.commit()

    return True, "", item


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
        user_id=user.id,
        class_slug=class_slug,
        class_name=class_name,
        coach=coach,
        time=time,
        price=price,
        enrolled_at=now,
        status="active",
    )
    db.session.add(enrollment)
    db.session.commit()
    return enrollment


# ---------------------------
# Event helpers (public + wallet)
# ---------------------------


def register_for_event(
    event_slug: str,
    event_title: str,
    user_id: Optional[str | int],
    name: str,
    email: str,
) -> EventRegistration:
    """
    Public/guest registration (or logged-in without wallet payment).
    Price is assumed 0 here.
    """
    uid: Optional[int] = None
    if user_id is not None:
        try:
            uid = int(user_id)
        except (TypeError, ValueError):
            uid = None

    # If user exists and name/email are blank, fill from profile
    if uid is not None and (not name or not email):
        u = User.query.get(uid)
        if u:
            if not name:
                name = (u.first_name or "") + " " + (u.last_name or "")
            if not email:
                email = u.email

    reg = EventRegistration(
        user_id=uid,
        event_slug=event_slug,
        title=event_title,
        name=name,
        email=email,
        price=0,
        status="registered",
    )
    db.session.add(reg)
    db.session.commit()
    return reg


def create_event_registration(
    user_id: str | int,
    event_slug: str,
    title: str,
    price: int,
) -> EventRegistration:
    """
    Wallet-based event registration for authenticated users.
    Name/email are taken from the user profile when available.
    """
    try:
        uid = int(user_id)
    except (TypeError, ValueError):
        uid = None

    name = ""
    email = ""
    if uid is not None:
        u = User.query.get(uid)
        if u:
            name = (u.first_name or "") + " " + (u.last_name or "")
            email = u.email

    reg = EventRegistration(
        user_id=uid,
        event_slug=event_slug,
        title=title,
        name=name,
        email=email,
        price=price,
        status="registered",
    )
    db.session.add(reg)
    db.session.commit()
    return reg


def count_event_registrations(event_slug: str) -> int:
    return EventRegistration.query.filter_by(
        event_slug=event_slug,
        status="registered",
    ).count()


def get_user_event_registrations(user_id: str | int) -> List[EventRegistration]:
    try:
        uid = int(user_id)
    except (TypeError, ValueError):
        return []
    return EventRegistration.query.filter_by(user_id=uid).all()


def user_is_registered_for_event(user_id: str | int, event_slug: str) -> bool:
    try:
        uid = int(user_id)
    except (TypeError, ValueError):
        return False

    return (
        EventRegistration.query.filter_by(
            user_id=uid,
            event_slug=event_slug,
            status="registered",
        ).first()
        is not None
    )
