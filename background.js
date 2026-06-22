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
  master: "",
  method: "",
  type: "",
  time: 0,
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

  session.master = details.url;
  session.method = details.method;
  session.type = details.type;
  session.time = Date.now();

  console.log(session);
}

// =====================
// Event
// =====================

browser.webRequest.onBeforeRequest.addListener((details) => {}, {
  urls: ["<all_urls>"],
});

///////อันก่อนหน้า////////////////////////////////////////////////////////////////
/*
let lastMaster = "";

browser.webRequest.onBeforeRequest.addListener(
  (details) => {
    if (!details.url.includes("master.m3u8")) return;

    lastMaster = details.url;

    console.log("⭐", lastMaster);
  },
  {
    urls: ["<all_urls>"],
  },
);
*/
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
