// t translates UI text with the dictionary the server put into window.I18N
// (keys are the English text) and fills %s / %d placeholders in order.
window.t = (s, ...args) => {
  let out = (window.I18N && window.I18N[s]) || s;
  for (const a of args) out = out.replace(/%[sd]/, String(a));
  return out;
};
