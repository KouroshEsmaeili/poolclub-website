// app.js
// Central JS for booking, classes, navbar, live rankings, and events

(function () {
  "use strict";

  document.addEventListener("DOMContentLoaded", initApp);

  function initApp() {
    initBookingModal();
    initClassesModal();
    initNavbar();
    initLiveRankings();
    initLandingEventRegistration();
    initDashboardEventWalletRegistration();
  }

  /* =========================
   *  Booking Modal Logic (استخر / لاین)
   * ========================= */
  function initBookingModal() {
    const bookingForm = document.getElementById("bookingForm");
    const bookingMessage = document.getElementById("bookingMessage");
    const bookingPricePreview = document.getElementById("bookingPricePreview");
    const bookingModal = document.getElementById("bookingModal");

    if (!bookingForm) {
      return; // no booking UI on this page
    }

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
      bookingMessage.classList.remove("d-none", "alert-success", "alert-danger", "alert-warning");
      bookingMessage.classList.add(`alert-${type}`);
      bookingMessage.textContent = text;
    }

    function clearMessage() {
      if (!bookingMessage) return;
      bookingMessage.classList.add("d-none");
      bookingMessage.textContent = "";
    }

    function updatePrice() {
      if (!bookingPricePreview) return;

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

    // Attach listeners
    if (bookingForm.type) {
      bookingForm.type.addEventListener("change", updatePrice);
    }
    if (bookingForm.duration) {
      bookingForm.duration.addEventListener("change", updatePrice);
    }
    updatePrice();

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
        body: JSON.stringify({ date, time, duration, type }),
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
   *  Classes – modal + signup
   * ========================= */
  function initClassesModal() {
    const classCards = document.querySelectorAll(".class-card");
    const signupBtn = document.getElementById("classModalSignupButton");

    if (!classCards.length && !signupBtn) {
      return; // no classes UI on this page
    }

    classCards.forEach(function (card) {
      card.addEventListener("click", function () {
        const {
          name,
          coach,
          time,
          capacity,
          price,
          description,
          slug,
        } = card.dataset;

        const titleEl = document.getElementById("classModalTitle");
        const coachEl = document.getElementById("classModalCoach");
        const timeEl = document.getElementById("classModalTime");
        const capacityEl = document.getElementById("classModalCapacity");
        const priceEl = document.getElementById("classModalPrice");
        const descriptionEl = document.getElementById("classModalDescription");
        const slugInput = document.getElementById("classModalSlug");

        if (titleEl) titleEl.textContent = name || "";
        if (coachEl) coachEl.textContent = coach || "";
        if (timeEl) timeEl.textContent = time || "";
        if (capacityEl) capacityEl.textContent = capacity || "";
        if (priceEl) priceEl.textContent = price || "";
        if (descriptionEl) descriptionEl.textContent = description || "";
        if (slugInput) slugInput.value = slug || "";

        const modalEl = document.getElementById("classDetailModal");
        if (modalEl && typeof bootstrap !== "undefined") {
          const modal = bootstrap.Modal.getOrCreateInstance(modalEl);
          modal.show();
        }
      });

      // keyboard accessibility
      card.addEventListener("keydown", function (e) {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          card.click();
        }
      });
    });

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
          body: JSON.stringify({ class_slug: slug }),
        })
          .then((res) => res.json().then((data) => ({ status: res.status, body: data })))
          .then(({ status, body }) => {
            signupBtn.disabled = false;
            signupBtn.textContent = "ثبت‌نام در این کلاس";

            if (body.status === "error" || status >= 400) {
              alert(body.message || "خطا در ثبت‌نام کلاس.");
              return;
            }

            alert(body.message || "ثبت‌نام با موفقیت انجام شد.");
            window.location.href = "/dashboard/classes";
          })
          .catch((err) => {
            console.error(err);
            signupBtn.disabled = false;
            signupBtn.textContent = "ثبت‌نام در این کلاس";
            alert("خطا در ارتباط با سرور. لطفاً دوباره تلاش کنید.");
          });
      });
    }
  }

  /* =========================
   *  Navbar background logic
   * ========================= */
  function initNavbar() {
    handleNavbarShrink();
    updateNavLinkColors();

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
  }

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

  function updateNavLinkColors() {
    const nav = document.getElementById("mainNav");
    if (!nav) return;

    const bg = getEffectiveBgColor(nav);
    const light = isLightColor(bg);

    nav.querySelectorAll(".nav-link").forEach((link) => {
      link.classList.toggle("nav-link-light", !light); // dark bg → light text
      link.classList.toggle("nav-link-dark", light);   // light bg → dark text
    });
  }

  /* =========================
   *  Live rankings refresh
   * ========================= */
  function initLiveRankings() {
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

    // initial load + periodic refresh
    refreshRankings();
    setInterval(refreshRankings, 60000);
  }

  /* =========================
   *  Landing page: event registration modal
   * ========================= */
  function initLandingEventRegistration() {
    const eventModalEl = document.getElementById("eventRegisterModal");
    const eventTitleEl = document.getElementById("eventRegisterTitle");
    const eventSlugInput = document.getElementById("eventRegisterSlug");
    const eventNameInput = document.getElementById("eventRegisterName");
    const eventEmailInput = document.getElementById("eventRegisterEmail");
    const eventForm = document.getElementById("eventRegisterForm");
    const eventMsg = document.getElementById("eventRegisterMessage");
    const eventSubmitBtn = document.getElementById("eventRegisterSubmit");

    // if this form is not present, skip
    if (!eventForm && !eventModalEl) return;

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

    // Attach click handler to "شرکت در رویداد" buttons on landing page
    document.querySelectorAll(".js-event-register-btn").forEach((btn) => {
      btn.addEventListener("click", function () {
        const slug = this.dataset.slug || "";
        const title = this.dataset.title || "";

        if (eventSlugInput) eventSlugInput.value = slug;
        if (eventTitleEl) eventTitleEl.textContent = title;

        clearEventMessage();
        if (eventForm) eventForm.reset();

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
          body: JSON.stringify({ event_slug: slug, name, email }),
        })
          .then((res) => res.json().then((data) => ({ status: res.status, body: data })))
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
          .catch((err) => {
            console.error(err);
            if (eventSubmitBtn) {
              eventSubmitBtn.disabled = false;
              eventSubmitBtn.textContent = "تأیید ثبت‌نام";
            }
            showEventMessage("danger", "خطا در ارتباط با سرور. لطفاً دوباره تلاش کنید.");
          });
      });
    }
  }

  /* =========================
   *  Dashboard: event registration with wallet
   * ========================= */
  function initDashboardEventWalletRegistration() {
    const walletButtons = document.querySelectorAll(".js-event-register-wallet-btn");
    if (!walletButtons.length) return;

    walletButtons.forEach((btn) => {
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
  }

  /**
   * Global helper for dashboard event registration with wallet.
   * Uses /api/events/register with JSON { slug }.
   * On success: reloads page to reflect new status & wallet balance.
   */
  function registerForEvent(slug) {
    if (!slug) return;

    fetch("/api/events/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ slug: slug }),
    })
      .then((res) => res.json().then((data) => ({ status: res.status, body: data })))
      .then(({ status, body }) => {
        if (body.status === "error" || status >= 400) {
          alert(body.message || "خطا در ثبت‌نام رویداد");
          return;
        }

        // Simple: reload to update UI (badges, counts, wallet)
        window.location.reload();
      })
      .catch((err) => {
        console.error(err);
        alert("خطا در ارتباط با سرور");
      });
  }

  // expose for any inline template calls
  window.registerForEvent = registerForEvent;
})();
