// رزرو سانس - فرم مودال
document.getElementById('bookingForm')?.addEventListener('submit', function (e) {
  e.preventDefault();
  const f = new FormData(this);
  alert(
    `درخواست رزرو:\n` +
    `${f.get('date')}  ${f.get('time')} · ${f.get('duration')} دقیقه · ${f.get('type')}\n\n` +
    `(گام بعدی: اتصال به /api/availability با داده ماک JSON)`
  );
});

// کلاس‌ها – باز کردن مودال با کلیک روی کارت
document.querySelectorAll('.class-card').forEach(function (card) {
  card.addEventListener('click', function () {
    const name = this.dataset.name;
    const coach = this.dataset.coach;
    const time = this.dataset.time;
    const capacity = this.dataset.capacity;
    const price = this.dataset.price;
    const description = this.dataset.description;

    // پر کردن محتوای مودال
    document.getElementById('classModalTitle').textContent = name;
    document.getElementById('classModalCoach').textContent = coach;
    document.getElementById('classModalTime').textContent = time;
    document.getElementById('classModalCapacity').textContent = capacity;
    document.getElementById('classModalPrice').textContent = price;
    document.getElementById('classModalDescription').textContent = description;

    const modalEl = document.getElementById('classDetailModal');
    if (modalEl) {
      const modal = bootstrap.Modal.getOrCreateInstance(modalEl);
      modal.show();
    }
  });

  // Optional: keyboard access (Enter key)
  card.addEventListener('keydown', function (e) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      this.click();
    }
  });
});

// رفتار ابتدایی برای دکمه‌ی ثبت‌نام
const signupBtn = document.getElementById('classModalSignupButton');
if (signupBtn) {
  signupBtn.addEventListener('click', function () {
    const title = document.getElementById('classModalTitle').textContent || '';
    alert(
      `در نسخه بعدی، این دکمه شما را به صفحه ثبت‌نام برای کلاس:\n«${title}»\nمی‌بَرَد.`
    );
  });
}

/* =========================
 *  Navbar background logic
 * ========================= */

// تابع: کوچک کردن / پس‌زمینه‌دار کردن نوار بالا بعد از اسکرول
function handleNavbarShrink() {
  const navbar = document.getElementById('mainNav');
  if (!navbar) return;

  const SHRINK_OFFSET = 40; // چند پیکسل بعد از بالای صفحه

  if (window.scrollY > SHRINK_OFFSET) {
    navbar.classList.add('navbar-shrink');
  } else {
    navbar.classList.remove('navbar-shrink');
  }
}

// پیدا کردن رنگ پس‌زمینه‌ی موثر
function getEffectiveBgColor(el) {
  // Walk up the DOM until we find a non-transparent background
  while (el && el !== document.documentElement) {
    const color = window.getComputedStyle(el).backgroundColor;
    if (color && color !== "rgba(0, 0, 0, 0)" && color !== "transparent") {
      return color;
    }
    el = el.parentElement;
  }
  // fallback: body background
  return window.getComputedStyle(document.body).backgroundColor;
}

// تشخیص روشن/تیره بودن رنگ
function isLightColor(color) {
  // Expect formats like rgb(r,g,b) or rgba(r,g,b,a)
  const m = color.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/i);
  if (!m) {
    // If we can’t parse, assume light to avoid invisible text
    return true;
  }
  const r = parseInt(m[1], 10);
  const g = parseInt(m[2], 10);
  const b = parseInt(m[3], 10);
  // perceived brightness formula
  const brightness = (r * 299 + g * 587 + b * 114) / 1000;
  return brightness > 128;
}

// به‌روزرسانی رنگ لینک‌های منو بر اساس پس‌زمینه
function updateNavLinkColors() {
  const nav = document.getElementById("mainNav");
  if (!nav) return;

  const bg = getEffectiveBgColor(nav);
  const isLight = isLightColor(bg);

  nav.querySelectorAll(".nav-link").forEach(link => {
    link.classList.toggle("nav-link-light", !isLight); // dark bg → light text
    link.classList.toggle("nav-link-dark", isLight);   // light bg → dark text
  });
}

// راه‌اندازی رویدادها
document.addEventListener("DOMContentLoaded", function () {
  handleNavbarShrink();      // وضعیت اولیه (اگر کاربر وسط صفحه رفرش کند)
  updateNavLinkColors();
});

window.addEventListener("scroll", function () {
  handleNavbarShrink();
  updateNavLinkColors();
}, { passive: true });

window.addEventListener("resize", function () {
  handleNavbarShrink();
  updateNavLinkColors();
});
