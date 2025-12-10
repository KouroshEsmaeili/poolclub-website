/* =========================
 *  Booking Modal Logic (استخر / لاین)
 * ========================= */

document.addEventListener("DOMContentLoaded", function () {
  // عناصر مودال رزرو
  const bookingForm = document.getElementById("bookingForm");
  const bookingMessage = document.getElementById("bookingMessage");
  const bookingPricePreview = document.getElementById("bookingPricePreview");
  const bookingModal = document.getElementById("bookingModal");
  
  
  const PRICES = window.BOOKING_PRICES
      ? {
          "شنای آزاد": window.BOOKING_PRICES.free_swim || 0,
          "رزرو لاین تمرین": window.BOOKING_PRICES.lane_training || 0,
        }
      : {
          // fallback if backend variable is missing
          "شنای آزاد": 40000,
          "رزرو لاین تمرین": 80000,
        };
    
    function showMessage(type, text) {
    if (!bookingMessage) return;
    bookingMessage.classList.remove("d-none");
    bookingMessage.classList.remove("alert-success", "alert-danger", "alert-warning");
    bookingMessage.classList.add(`alert-${type}`);
    bookingMessage.textContent = text;
  }

  function clearMessage() {
    if (!bookingMessage) return;
    bookingMessage.classList.add("d-none");
  }

  function updatePrice() {
    if (!bookingForm || !bookingPricePreview) return;

    const type = bookingForm.type ? bookingForm.type.value.trim() : "";
    const basePrice = PRICES[type] || 0;

    const duration = bookingForm.duration
      ? parseInt(bookingForm.duration.value, 10)
      : 60;

    const multiplier = duration / 60; // 60, 90, 120 → 1, 1.5, 2
    const finalPrice = Math.round(basePrice * multiplier);

    bookingPricePreview.textContent = finalPrice
      ? finalPrice.toLocaleString("fa-IR") + " تومان"
      : "–";
  }

  // اگر فرم وجود دارد، لیسنرها را وصل کن
  if (bookingForm) {
    if (bookingForm.type) {
      bookingForm.type.addEventListener("change", updatePrice);
    }
    if (bookingForm.duration) {
      bookingForm.duration.addEventListener("change", updatePrice);
    }
    updatePrice();

    // هندلر ارسال فرم → /api/bookings/create
    bookingForm.addEventListener("submit", function (e) {
      e.preventDefault();
      clearMessage();

      const formData = new FormData(bookingForm);
      const date = formData.get("date");
      const time = formData.get("time");
      const duration = parseInt(formData.get("duration"), 10);
      const type = formData.get("type");

      if (!date || !time) {
        showMessage("warning", "لطفاً تاریخ و ساعت را وارد کنید.");
        return;
      }

      const submitBtn = bookingForm.querySelector("button[type='submit']");
      if (submitBtn) {
        submitBtn.disabled = true;
        submitBtn.textContent = "در حال بررسی...";
      }

      fetch("/api/bookings/create", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          date,
          time,
          duration,
          type
        })
      })
        .then((res) => res.json())
        .then((data) => {
          if (submitBtn) {
            submitBtn.disabled = false;
            submitBtn.textContent = "بررسی ظرفیت و ثبت رزرو";
          }

          if (data.status === "error") {
            showMessage("danger", data.message || "خطایی رخ داد.");
            return;
          }

          if (data.status === "success") {
            showMessage("success", "رزرو با موفقیت ثبت شد!");

            setTimeout(() => {
              if (bookingModal && typeof bootstrap !== "undefined") {
                let modalInstance = bootstrap.Modal.getInstance(bookingModal);
                if (!modalInstance) {
                  modalInstance = bootstrap.Modal.getOrCreateInstance(bookingModal);
                }
                modalInstance.hide();
              }
              window.location.href = "/dashboard/bookings";
            }, 1100);
          }
        })
        .catch((err) => {
          console.error(err);
          showMessage("danger", "خطا در ارتباط با سرور. لطفاً دوباره تلاش کنید.");
          if (submitBtn) {
            submitBtn.disabled = false;
            submitBtn.textContent = "بررسی ظرفیت و ثبت رزرو";
          }
        });
    });
  }

  /* =========================
   *  کلاس‌ها – باز کردن مودال با کلیک روی کارت
   *  (برنامه‌ها/کلاس‌ها در روت جداگانه هندل می‌شوند، این فقط نمایش است)
   * ========================= */

  document.querySelectorAll(".class-card").forEach(function (card) {
    card.addEventListener("click", function () {
      const name = this.dataset.name;
      const coach = this.dataset.coach;
      const time = this.dataset.time;
      const capacity = this.dataset.capacity;
      const price = this.dataset.price;
      const description = this.dataset.description;

      const titleEl = document.getElementById("classModalTitle");
      const coachEl = document.getElementById("classModalCoach");
      const timeEl = document.getElementById("classModalTime");
      const capacityEl = document.getElementById("classModalCapacity");
      const priceEl = document.getElementById("classModalPrice");
      const descriptionEl = document.getElementById("classModalDescription");
      const slug = this.dataset.slug;
      document.getElementById("classModalSlug").value = slug || "";
      
      if (titleEl) titleEl.textContent = name || "";
      if (coachEl) coachEl.textContent = coach || "";
      if (timeEl) timeEl.textContent = time || "";
      if (capacityEl) capacityEl.textContent = capacity || "";
      if (priceEl) priceEl.textContent = price || "";
      if (descriptionEl) descriptionEl.textContent = description || "";

      const modalEl = document.getElementById("classDetailModal");
      if (modalEl && typeof bootstrap !== "undefined") {
        const modal = bootstrap.Modal.getOrCreateInstance(modalEl);
        modal.show();
      }
    });

    // دسترسی با کیبورد
    card.addEventListener("keydown", function (e) {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        this.click();
      }
    });
  });

  // دکمه ثبت‌نام کلاس – فعلاً فقط پیام نمایشی
const signupBtn = document.getElementById("classModalSignupButton");
if (signupBtn) {
  signupBtn.addEventListener("click", function () {
    const isAuth = this.dataset.authenticated === "1";
    const loginUrl = this.dataset.loginUrl || "/login";

    if (!isAuth) {
      window.location.href = loginUrl;
      return;
    }

    const titleEl = document.getElementById("classModalTitle");
    const slugEl = document.getElementById("classModalSlug");

    const className = titleEl ? (titleEl.textContent || "").trim() : "";
    const slug = slugEl ? (slugEl.value || "").trim() : "";

    if (!slug) {
      alert("کلاس معتبر یافت نشد.");
      return;
    }

    signupBtn.disabled = true;
    signupBtn.textContent = "در حال ثبت‌نام...";

    fetch("/api/classes/enroll", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ class_slug: slug })
    })
      .then(res => res.json().then(data => ({ status: res.status, body: data })))
      .then(({ status, body }) => {
        signupBtn.disabled = false;
        signupBtn.textContent = "ثبت‌نام در این کلاس";

        if (body.status === "error" || status >= 400) {
          alert(body.message || "خطا در ثبت‌نام کلاس.");
          return;
        }

        alert(body.message || "ثبت‌نام با موفقیت انجام شد.");

        // انتقال به داشبورد کلاس‌ها
        window.location.href = "/dashboard/classes";
      })
      .catch(err => {
        console.error(err);
        signupBtn.disabled = false;
        signupBtn.textContent = "ثبت‌نام در این کلاس";
        alert("خطا در ارتباط با سرور. لطفاً دوباره تلاش کنید.");
      });
  });
}


  // وضعیت اولیه ناوبار
  handleNavbarShrink();
  updateNavLinkColors();
});

/* =========================
 *  Navbar background logic
 * ========================= */

// کوچک کردن / پس‌زمینه‌دار کردن نوار بالا بعد از اسکرول
function handleNavbarShrink() {
  const navbar = document.getElementById("mainNav");
  if (!navbar) return;

  const SHRINK_OFFSET = 40;

  if (window.scrollY > SHRINK_OFFSET) {
    navbar.classList.add("navbar-shrink");
  } else {
    navbar.classList.remove("navbar-shrink");
  }
}

// پیدا کردن رنگ پس‌زمینه‌ی موثر
function getEffectiveBgColor(el) {
  while (el && el !== document.documentElement) {
    const color = window.getComputedStyle(el).backgroundColor;
    if (color && color !== "rgba(0, 0, 0, 0)" && color !== "transparent") {
      return color;
    }
    el = el.parentElement;
  }
  return window.getComputedStyle(document.body).backgroundColor;
}

// تشخیص روشن/تیره بودن رنگ
function isLightColor(color) {
  const m = color.match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/i);
  if (!m) {
    return true;
  }
  const r = parseInt(m[1], 10);
  const g = parseInt(m[2], 10);
  const b = parseInt(m[3], 10);
  const brightness = (r * 299 + g * 587 + b * 114) / 1000;
  return brightness > 128;
}

// به‌روزرسانی رنگ لینک‌های منو بر اساس پس‌زمینه
function updateNavLinkColors() {
  const nav = document.getElementById("mainNav");
  if (!nav) return;

  const bg = getEffectiveBgColor(nav);
  const isLight = isLightColor(bg);

  nav.querySelectorAll(".nav-link").forEach((link) => {
    link.classList.toggle("nav-link-light", !isLight); // پس‌زمینه تیره → متن روشن
    link.classList.toggle("nav-link-dark", isLight);   // پس‌زمینه روشن → متن تیره
  });
}

// رویدادهای اسکرول و تغییر اندازه
window.addEventListener(
  "scroll",
  function () {
    handleNavbarShrink();
    updateNavLinkColors();
  },
  { passive: true }
);

window.addEventListener("resize", function () {
  handleNavbarShrink();
  updateNavLinkColors();
});

document.addEventListener("DOMContentLoaded", function () {
  const tableBody = document.querySelector("#live-rankings table tbody");
  const updatedAtEl = document.querySelector("#live-rankings .text-muted.small");

  if (!tableBody) return;

  function renderRankings(items, updatedAt) {
    tableBody.innerHTML = "";
    items.slice(0, 10).forEach((item, index) => {
      const tr = document.createElement("tr");
      if (index < 3) tr.classList.add("table-warning");
      tr.innerHTML = `
        <td>${item.rank}</td>
        <td>${item.name}</td>
        <td>${item.club}</td>
        <td>${item.age_group}</td>
        <td>${item.stroke}</td>
        <td class="fw-bold">${item.score}</td>
      `;
      tableBody.appendChild(tr);
    });

    if (updatedAtEl && updatedAt) {
      updatedAtEl.textContent = "آخرین به‌روزرسانی: " + updatedAt;
    }
  }

  function refreshRankings() {
    fetch("/api/live-rankings")
      .then((res) => res.json())
      .then((data) => {
        if (data.status === "success") {
          renderRankings(data.items, data.updated_at);
        }
      })
      .catch((err) => console.error("live rankings error", err));
  }

  // Refresh every 60 seconds
  setInterval(refreshRankings, 60000);
});


document.addEventListener("DOMContentLoaded", function () {
  // --- Event registration (landing page) ---
  const eventModalEl = document.getElementById("eventRegisterModal");
  const eventTitleEl = document.getElementById("eventRegisterTitle");
  const eventSlugInput = document.getElementById("eventRegisterSlug");
  const eventNameInput = document.getElementById("eventRegisterName");
  const eventEmailInput = document.getElementById("eventRegisterEmail");
  const eventForm = document.getElementById("eventRegisterForm");
  const eventMsg = document.getElementById("eventRegisterMessage");
  const eventSubmitBtn = document.getElementById("eventRegisterSubmit");

  function showEventMessage(type, text) {
    if (!eventMsg) return;
    eventMsg.classList.remove("d-none", "alert-success", "alert-danger");
    eventMsg.classList.add("alert-" + type);
    eventMsg.textContent = text;
  }

  function clearEventMessage() {
    if (!eventMsg) return;
    eventMsg.classList.add("d-none");
    eventMsg.textContent = "";
  }

  // Attach click handler to "شرکت در رویداد" buttons
  document.querySelectorAll(".js-event-register-btn").forEach(btn => {
    btn.addEventListener("click", function () {
      const slug = this.dataset.slug || "";
      const title = this.dataset.title || "";

      if (eventSlugInput) eventSlugInput.value = slug;
      if (eventTitleEl) eventTitleEl.textContent = title;

      clearEventMessage();
      if (eventForm) eventForm.reset();

      // Optional: if you want, prefill name/email for logged-in user
      // You can render data attributes from Jinja if needed.

      if (eventModalEl && typeof bootstrap !== "undefined") {
        const modal = bootstrap.Modal.getOrCreateInstance(eventModalEl);
        modal.show();
      }
    });
  });

  if (eventForm) {
    eventForm.addEventListener("submit", function (e) {
      e.preventDefault();
      clearEventMessage();

      const slug = eventSlugInput ? eventSlugInput.value.trim() : "";
      const name = eventNameInput ? eventNameInput.value.trim() : "";
      const email = eventEmailInput ? eventEmailInput.value.trim() : "";

      if (!slug) {
        showEventMessage("danger", "رویداد نامعتبر است.");
        return;
      }
      if (!name || !email) {
        showEventMessage("danger", "لطفاً نام و ایمیل خود را وارد کنید.");
        return;
      }

      if (eventSubmitBtn) {
        eventSubmitBtn.disabled = true;
        eventSubmitBtn.textContent = "در حال ثبت...";
      }

      fetch("/api/events/public-register", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ event_slug: slug, name, email })
      })
        .then(res => res.json().then(data => ({ status: res.status, body: data })))
        .then(({ status, body }) => {
          if (eventSubmitBtn) {
            eventSubmitBtn.disabled = false;
            eventSubmitBtn.textContent = "تأیید ثبت‌نام";
          }

          if (body.status === "error" || status >= 400) {
            showEventMessage("danger", body.message || "خطا در ثبت‌نام رویداد.");
            return;
          }

          showEventMessage("success", body.message || "ثبت‌نام با موفقیت انجام شد.");
        })
        .catch(err => {
          console.error(err);
          if (eventSubmitBtn) {
            eventSubmitBtn.disabled = false;
            eventSubmitBtn.textContent = "تأیید ثبت‌نام";
          }
          showEventMessage("danger", "خطا در ارتباط با سرور. لطفاً دوباره تلاش کنید.");
        });
    });
  }
});

document.querySelectorAll(".js-event-register-wallet-btn").forEach(btn => {
  btn.addEventListener("click", function () {
    const slug = this.dataset.slug;
    const title = this.dataset.title;
    if (!slug) return;

    if (!confirm(`ثبت‌نام در رویداد:\n«${title}» ؟`)) {
      return;
    }
    registerForEvent(slug);
  });
});
