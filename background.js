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
    if (details.url.includes("master.m3u8")) {
      console.log("⭐", "FOUND MASTER");
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
