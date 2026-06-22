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

let session = {
  requestId: "", //
  master: "", //
  method: "", //
  type: "", //

  headers: [], //

  referer: "",
  userAgent: "",
  origin: "",
  cookies: "",

  tabId: 0,
  time: 0, //
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
  for (const header of details.requestHeaders) {
    session.headers[header.name] = header.value;
  }
  console.log(session);
}
/**

session.headers["Referer"] = "...";
session.headers["Origin"] = "...";
session.headers["User-Agent"] = "...";

{
    "User-Agent": "...",
    "Referer": "...",
    "Origin": "...",
    "Cookie": "..."
}
 
 */
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

/*
background.js

1. Const
2. Session
3. Helper Functions
4. Event

background.js
│
├── isIgnored()
├── isMaster()
├── saveMaster()
├── session
└── onBeforeRequest()

*/
