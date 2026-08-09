"use strict";

// Builds the capture bookmarklet with this instance's own origin baked in, so
// the link a user drags to their bookmarks bar already points at their tracker.
// Nothing here is hardcoded — the origin comes from wherever this page is served.

// Source of the bookmarklet, kept readable rather than pre-minified. __BASE__
// is substituted below. Runs on arbitrary third-party pages, so it: touches no
// globals, swallows its own errors, and only ever hands data to a URL.
function capture(BASE, PRESET) {
  PRESET = PRESET || {};
  function jobPosting() {
    var found = null;
    var nodes = document.querySelectorAll('script[type="application/ld+json"]');
    for (var i = 0; i < nodes.length && !found; i++) {
      var data;
      try { data = JSON.parse(nodes[i].textContent); } catch (e) { continue; }
      var list = Array.isArray(data) ? data : (data["@graph"] || [data]);
      for (var j = 0; j < list.length; j++) {
        var o = list[j];
        if (!o) continue;
        var t = o["@type"];
        if (t === "JobPosting" || (Array.isArray(t) && t.indexOf("JobPosting") >= 0)) {
          found = o;
          break;
        }
      }
    }
    return found || {};
  }

  // Descriptions are HTML. Render to text rather than shipping tags into a
  // notes field — and never insert this into the page.
  function plain(html) {
    var d = document.createElement("div");
    // textContent runs block elements together ("<li>a</li><li>b</li>" -> "ab"),
    // so give the closing tags a separator before parsing.
    d.innerHTML = String(html || "").replace(/<\/(p|li|div|h[1-6]|tr)>|<br\s*\/?>/gi, " $& ");
    return (d.textContent || "").replace(/\s+/g, " ").trim();
  }

  function meta(prop) {
    var m = document.querySelector('meta[property="' + prop + '"]');
    return m ? m.getAttribute("content") : "";
  }

  function salary(j) {
    var b = j.baseSalary;
    if (!b) return "";
    var v = b.value || {};
    var amount = v.value != null ? String(v.value)
      : (v.minValue != null ? v.minValue + (v.maxValue != null ? "-" + v.maxValue : "") : "");
    if (!amount) return "";
    var unit = v.unitText ? " / " + String(v.unitText).toLowerCase() : "";
    return amount + (b.currency ? " " + b.currency : "") + unit;
  }

  // Where did we come from? LinkedIn routes outbound links through a redirect
  // and sets a referrer policy, so this often arrives empty — treat it as a
  // bonus, never a requirement. The preset is the reliable path.
  function channelFromReferrer() {
    var r = document.referrer || "";
    if (/(^|\.)linkedin\.com/i.test(r)) return "LinkedIn";
    if (/(^|\.)(mail\.google|outlook)\./i.test(r)) return "Email";
    return "";
  }

  var j = jobPosting();
  var org = j.hiringOrganization || {};
  var params = {
    add: "application",
    entity: (typeof org === "string" ? org : org.name) || meta("og:site_name") ||
      location.hostname.replace(/^www\./, ""),
    context: j.title || meta("og:title") || document.title,
    link: j.url || location.href,
    comp: salary(j),
    notes: plain(j.description).slice(0, 1200),
    channel: PRESET.channel || channelFromReferrer(),
    status: PRESET.status || ""
  };
  if (typeof j.datePosted === "string" && /^\d{4}-\d{2}-\d{2}/.test(j.datePosted)) {
    params.date = j.datePosted.slice(0, 10);
  }

  function go() {
    var url = new URL(BASE);
    url.pathname = "/";
    Object.keys(params).forEach(function (k) {
      if (params[k]) url.searchParams.set(k, params[k]);
    });
    window.open(url.toString(), "_blank", "noopener");
  }

  // Workday and friends render their JSON-LD after first paint, so an
  // immediate read returns nothing and looks like "this site is unsupported".
  // Give it a couple of short retries before falling back to the page title.
  if (!j.title && !j.hiringOrganization) {
    var tries = 0;
    var timer = setInterval(function () {
      var late = jobPosting();
      if (late.title || late.hiringOrganization || ++tries >= 6) {
        clearInterval(timer);
        if (late.title) params.context = late.title;
        var lateOrg = late.hiringOrganization;
        if (lateOrg) params.entity = (typeof lateOrg === "string" ? lateOrg : lateOrg.name) || params.entity;
        if (late.url) params.link = late.url;
        if (late.description) params.notes = plain(late.description).slice(0, 1200);
        var lateComp = salary(late);
        if (lateComp) params.comp = lateComp;
        go();
      }
    }, 250);
    return;
  }
  go();
}

(function build() {
  var origin = location.origin;
  document.getElementById("origin").textContent = origin;

  function link(id, preset) {
    var src = "(" + capture.toString() + ")(" +
      JSON.stringify(origin) + "," + JSON.stringify(preset) + ")";
    document.getElementById(id).href = "javascript:" + encodeURIComponent(src);
  }
  link("bm", {});
  link("bm-linkedin", { channel: "LinkedIn", status: "Applied" });
})();
