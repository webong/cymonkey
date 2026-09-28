// Click again to remove this bookmarklet's style. No permanent page changes.
const id = 'jangolova-bookmarklet-highlight-links';
const previous = document.getElementById(id);
if (previous) {
  previous.remove();
} else {
  const style = document.createElement('style');
  style.id = id;
  style.textContent = 'a[href] { outline: 2px solid #e59b00 !important; outline-offset: 2px !important; }';
  (document.head || document.documentElement).appendChild(style);
}
