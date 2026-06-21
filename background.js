// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.

browser.webRequest.onBeforeRequest.addListener(
  (details) => {
    //ถ้าเจอให้แสดง
    if (details.url.includes(".m3u8")) {
      console.log("⭐", "m3u8");
    }

    if (details.url.includes("master.m3u8")) {
      console.log("⭐", "MASTER");
    }

    if (details.url.includes("480p.m3u8")) {
      console.log("⭐", "480p");
    }

    if (details.url.includes("720p.m3u8")) {
      console.log("⭐", "720p");
    }

    if (details.url.includes("1080p.m3u8")) {
      console.log("⭐", "1080p");
    }

    if (details.url.includes("keys")) {
      console.log("⭐", "KEYS");
    }

    console.log({
      type: details.type,
      method: details.method,
      url: details.url,
    });
  },
  {
    urls: ["<all_urls>"],
  },
);
