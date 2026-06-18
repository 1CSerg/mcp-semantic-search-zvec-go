;; Functions
(function_declaration
  name: (identifier) @name) @boundary.function

;; Methods
(method_declaration
  receiver: (parameter_list (parameter_declaration type: (_) @scope.receiver))
  name: (field_identifier) @name) @boundary.method

;; Types — each type_spec is a separate boundary (including inside type (...))
(type_spec
  name: (type_identifier) @name) @boundary.type

;; Constants — each const_spec is a separate boundary (including inside const (...))
(const_spec
  name: (identifier) @name) @boundary.const

;; Variables — each var_spec is a separate boundary (including inside var (...))
(var_spec
  name: (identifier) @name) @boundary.var

;; Imports are not boundaries — they accumulate in the cAST buffer (§5 edge case 2).

;; Package (for scope extraction)
(source_file
  (package_clause
    (package_identifier) @scope.package))
