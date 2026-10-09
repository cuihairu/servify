module.exports = {
  extends: [require.resolve('@umijs/max/eslint')],
  rules: {
    // `x != null` / `x == null` 是「非 null 且非 undefined」的惯用判断
    // （可选字段渲染分支），只放行对 null 的宽松比较，其余等值仍严格。
    eqeqeq: ['error', 'always', { null: 'ignore' }],
  },
};
