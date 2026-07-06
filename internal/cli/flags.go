package cli

// Flag state shared by the root command and its subcommands, mirroring the
// yq CLI surface (subset: yaml/json) plus gq-specific flags added in later
// phases (--values, --values-file, --all, --gotmpl).
var (
	verbose         bool
	version         bool
	writeInplace    bool
	outputFormat    string
	inputFormat     string
	indent          int
	prettyPrint     bool
	exitStatus      bool
	nullInput       bool
	noDocSeparators bool
	forceColor      bool
	forceNoColor    bool
	frontMatter     string
	forceExpression string
	expressionFile  string
	forceGotmpl     bool
	noGotmpl        bool
	allBranches     bool
	valuesExprs     []string
	valuesFiles     []string

	// resolved during initCommand
	colorsEnabled bool
	unwrapScalar  bool
	// raw value of -r/--unwrapScalar; only applied when explicitly set,
	// otherwise the default depends on the output format (true for yaml)
	unwrapScalarFlag bool

	completedSuccessfully bool
)
