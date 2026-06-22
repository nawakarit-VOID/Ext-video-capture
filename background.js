// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.

// =====================
// Const
// =====================

const ignore = [
  ".css",
  ".png",
  ".jpg",
  ".jpeg",
  ".gif",
  ".svg",
  ".woff",
  ".woff2",
  ".ttf",
  ".ico",
];

// =====================
// Session
// =====================
//Global Variable
let session = {
  requestId: "", //⭐
  master: "", //⭐
  method: "", //⭐
  type: "", //⭐
  headers: [], //⭐
  tabId: 0,
  time: 0, //⭐
};

// =====================
// Helper
// =====================

function isIgnored(url) {
  //ไม่สนใจ
  url = url.toLowerCase();

  for (const ext of ignore) {
    if (url.includes(ext)) {
      return true;
    }
  }
  return false;
}

function isMaster(url) {
  //หา Master
  url = url.toLowerCase();
  //หรือ return url.toLowerCase().includes("master.m3u8");
  if (url.includes("master.m3u8")) {
    console.log("⭐", "MASTER");
    return true;
  }
  return false;
}

function saveMaster(details) {
  //บันทึก Master
  session.requestId = details.requestId;
  session.master = details.url;
  session.method = details.method;
  session.type = details.type;
  session.time = Date.now();

  console.log(session);
}

function saveHeaders(details) {
  //session.headers = details.requestHeaders;
  session.headers = {};
  //ถ้าไม่มี requestHeaders เข้ามา ให้ออกไปซะ (อี...)
  if (!details.requestHeaders) {
    return;
  }

  for (const header of details.requestHeaders) {
    //ปวดหัว (จุง...)
    session.headers[header.name] = header.value;
  }
  console.log(session);
}

// =====================
// Event
// =====================

browser.webRequest.onBeforeRequest.addListener(
  (details) => {
    //
    if (isIgnored(details.url)) {
      return;
    }

    if (isMaster(details.url)) {
      saveMaster(details);
    }
  },
  {
    urls: ["<all_urls>"],
  },
);

browser.webRequest.onBeforeSendHeaders.addListener(
  (details) => {
    if (details.requestId !== session.requestId) {
      return;
    }

    saveHeaders(details);
  },
  {
    urls: ["<all_urls>"],
  },
  ["requestHeaders"],
);

browser.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === "GET_SESSION") {
    console.log("🔍 Sending session:", session);

    sendResponse({
      success: true,
      session: session,
    });

    return true; // สำคัญมาก (หรอ)
  }
});
