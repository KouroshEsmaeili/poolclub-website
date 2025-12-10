// postcss.config.js

const autoprefixer = require('autoprefixer');
const purgecss = require('@fullhuman/postcss-purgecss').default;

module.exports = {
  plugins: [
    autoprefixer,
    purgecss({
      content: [
        './app/templates/**/*.html',
        './app/templates/*.html',
        './app/static/js/**/*.js',
        './app/auth.py',
        './app/routes.py'
      ],
      safelist: [
        // classes that are added dynamically or by JS / Flask
        /^alert-/,
        /^modal-/,
        /^nav-link-/,
        /^table-/,
        /^badge-/,
        /^btn-/,
        /^toast/,
        /^collapse/,
        /^show$/,
      ]
    })
    // If later you want minification, we can add cssnano back using an ESM-compatible setup.
  ],
};
