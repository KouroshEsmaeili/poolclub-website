from flask import Blueprint, render_template, request, redirect, url_for, flash
from flask_login import login_user, logout_user, login_required, current_user

from .model import get_user_by_email, create_user

auth = Blueprint("auth", __name__, url_prefix="/auth")


@auth.route("/login", methods=["GET", "POST"])
def login():
    # Already logged in → go to dashboard
    if current_user.is_authenticated:
        return redirect(url_for("main.user_dashboard"))

    if request.method == "POST":
        email = request.form.get("email", "").strip()
        password = request.form.get("password", "")

        user = get_user_by_email(email)
        if not user or not user.check_password(password):
            flash("ایمیل یا رمز عبور نادرست است.", "danger")
            return render_template("auth/login.html")

        login_user(user, remember=True)
        flash("با موفقیت وارد شدید.", "success")
        next_url = request.args.get("next") or url_for("main.user_dashboard")
        return redirect(next_url)

    return render_template("auth/login.html")


@auth.route("/register", methods=["GET", "POST"])
def register():
    # Already logged in → go to dashboard
    if current_user.is_authenticated:
        return redirect(url_for("main.user_dashboard"))

    if request.method == "POST":
        first_name = request.form.get("first_name", "").strip()
        last_name = request.form.get("last_name", "").strip()
        email = request.form.get("email", "").strip()
        password = request.form.get("password", "")
        password2 = request.form.get("password2", "")

        if not email or not password:
            flash("ایمیل و رمز عبور الزامی است.", "danger")
            return render_template("auth/register.html")

        if password != password2:
            flash("تکرار رمز عبور هم‌خوانی ندارد.", "danger")
            return render_template("auth/register.html")

        if get_user_by_email(email):
            flash("برای این ایمیل قبلاً حساب ساخته شده است.", "warning")
            return render_template("auth/register.html")

        user = create_user(
            email=email,
            password=password,
            first_name=first_name,
            last_name=last_name,
        )
        login_user(user)
        flash("ثبت‌نام با موفقیت انجام شد.", "success")
        return redirect(url_for("main.user_dashboard"))

    return render_template("auth/register.html")


@auth.route("/logout")
@login_required
def logout():
    logout_user()
    flash("با موفقیت خارج شدید.", "success")
    return redirect(url_for("main.index"))
