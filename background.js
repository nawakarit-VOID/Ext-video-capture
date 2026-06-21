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

const keywords = ["m3u8", "mpd", "token", "playlist", "manifest"];

browser.webRequest.onBeforeRequest.addListener(
  (details) => {
    const url = details.url.toLowerCase();

    for (const ext of ignore) {
      if (url.includes(ext)) {
        return;
      }
    }

    for (const k of keywords) {
      if (url.includes(k)) {
        console.log("⭐", details.url);
        return;
      }
    }
  },
  {
    urls: ["<all_urls>"],
  },
);
