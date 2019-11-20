# `terraform-clean-syntax`

`terraform-clean-syntax` is a simple command line tool for performing some
small syntax cleanup steps on Terraform `.tf` configuration files
automatically.

Specifically, it currently knows how to clean up the following:

* Argument values that are just a single template interpolation, like
  `"${foo}"`, are simplified to the equivalent `foo`.
* Variable type constraints using the legacy forms from Terraform 0.11, like
  `"string"`, `"list"`, or `"map"`, are replaced with their modern type
  constraint expressions `string`, `list(string)` and `map(string)`.

The two changes listed above will both silence some (though not all) of the
syntax deprecation warnings emitted by Terraform 0.12.14 and later. This program
is conservative, so it may skip certain opportunities for cleanup if they are
too complex for it to be sure that the change is safe.

The built in `terraform fmt` command in Terraform doesn't perform these cleanups
automatically at the time of writing, because the Terraform team worried that
this would make it difficult for folks to continue maintaining modules that
are cross-compatible with both Terraform 0.11 and 0.12. The cleanups made by
this program will render a configuration incompatible with Terraform 0.11,
so this program should not be used on any module that must retain Terraform 0.11
compatibility.

## Usage

After compiling the program using Go 1.12 or later, run it with a single
argument that is a file or directory to apply rewriting to:

```
terraform-clean-syntax .
```

If given a directory, `terraform-clean-syntax` will visit all of the `.tf`
files in the directory and recursively search any directories within it.

If given a single file, `terraform-clean-syntax` will process that file only
if its name has the suffix `.tf`.

This program rewrites configuration files in-place, so it's best to make sure
your version control work tree is clean before running so that you can clearly
see which changes it is proposing and discard those changes if desired.

This program is a best-effort static analysis tool and it doesn't have intimate
understanding of Terraform language syntax, so be sure to review the changes it
proposes and test your resulting configuration with `terraform validate` and/or
`terraform plan` before merging the changes into your codebase.
