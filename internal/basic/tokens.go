package basic

const (
	tokenEnd       = 0x80
	tokenFor       = 0x81
	tokenNext      = 0x82
	tokenData      = 0x83
	tokenInputHash = 0x84
	tokenInput     = 0x85
	tokenDim       = 0x86
	tokenRead      = 0x87
	tokenLet       = 0x88
	tokenGoto      = 0x89
	tokenRun       = 0x8a
	tokenIf        = 0x8b
	tokenRestore   = 0x8c
	tokenGosub     = 0x8d
	tokenReturn    = 0x8e
	tokenRem       = 0x8f
	tokenStop      = 0x90
	tokenOn        = 0x91
	tokenWait      = 0x92
	tokenLoad      = 0x93
	tokenSave      = 0x94
	tokenVerify    = 0x95
	tokenDef       = 0x96
	tokenPoke      = 0x97
	tokenPrintHash = 0x98
	tokenPrint     = 0x99
	tokenCont      = 0x9a
	tokenList      = 0x9b
	tokenClr       = 0x9c
	tokenCmd       = 0x9d
	tokenSys       = 0x9e
	tokenOpen      = 0x9f
	tokenClose     = 0xa0
	tokenGet       = 0xa1
	tokenNew       = 0xa2
	tokenTab       = 0xa3
	tokenTo        = 0xa4
	tokenFn        = 0xa5
	tokenSpc       = 0xa6
	tokenThen      = 0xa7
	tokenNot       = 0xa8
	tokenStep      = 0xa9
	tokenPlus      = 0xaa
	tokenMinus     = 0xab
	tokenMul       = 0xac
	tokenDiv       = 0xad
	tokenPow       = 0xae
	tokenAnd       = 0xaf
	tokenOr        = 0xb0
	tokenGT        = 0xb1
	tokenEQ        = 0xb2
	tokenLT        = 0xb3
	tokenSgn       = 0xb4
	tokenInt       = 0xb5
	tokenAbs       = 0xb6
	tokenUsr       = 0xb7
	tokenFre       = 0xb8
	tokenPos       = 0xb9
	tokenSqr       = 0xba
	tokenRnd       = 0xbb
	tokenLog       = 0xbc
	tokenExp       = 0xbd
	tokenCos       = 0xbe
	tokenSin       = 0xbf
	tokenTan       = 0xc0
	tokenAtn       = 0xc1
	tokenPeek      = 0xc2
	tokenLen       = 0xc3
	tokenStr       = 0xc4
	tokenVal       = 0xc5
	tokenAsc       = 0xc6
	tokenChr       = 0xc7
	tokenLeft      = 0xc8
	tokenRight     = 0xc9
	tokenMid       = 0xca
	tokenGo        = 0xcb
)

const (
	tokenSoundMasterOff        = 0xcc
	tokenSoundMasterIf         = 0xcd
	tokenSoundMasterVolume     = 0xce
	tokenSoundMasterWave       = 0xcf
	tokenSoundMasterEnvelope   = 0xd0
	tokenSoundMasterOscillate  = 0xd1
	tokenSoundMasterTune       = 0xd2
	tokenSoundMasterPlay       = 0xd3
	tokenSoundMasterFilter     = 0xd4
	tokenSoundMasterSoundClear = 0xd5
	tokenSoundMasterHelp       = 0xd6
)

func basicTokenName(token byte) string {
	switch token {
	case tokenEnd:
		return "END"
	case tokenFor:
		return "FOR"
	case tokenNext:
		return "NEXT"
	case tokenData:
		return "DATA"
	case tokenInputHash:
		return "INPUT#"
	case tokenInput:
		return "INPUT"
	case tokenDim:
		return "DIM"
	case tokenRead:
		return "READ"
	case tokenLet:
		return "LET"
	case tokenGoto:
		return "GOTO"
	case tokenRun:
		return "RUN"
	case tokenIf:
		return "IF"
	case tokenRestore:
		return "RESTORE"
	case tokenGosub:
		return "GOSUB"
	case tokenReturn:
		return "RETURN"
	case tokenRem:
		return "REM"
	case tokenStop:
		return "STOP"
	case tokenOn:
		return "ON"
	case tokenWait:
		return "WAIT"
	case tokenLoad:
		return "LOAD"
	case tokenSave:
		return "SAVE"
	case tokenVerify:
		return "VERIFY"
	case tokenDef:
		return "DEF"
	case tokenPoke:
		return "POKE"
	case tokenPrintHash:
		return "PRINT#"
	case tokenPrint:
		return "PRINT"
	case tokenCont:
		return "CONT"
	case tokenList:
		return "LIST"
	case tokenClr:
		return "CLR"
	case tokenCmd:
		return "CMD"
	case tokenSys:
		return "SYS"
	case tokenOpen:
		return "OPEN"
	case tokenClose:
		return "CLOSE"
	case tokenGet:
		return "GET"
	case tokenNew:
		return "NEW"
	case tokenTab:
		return "TAB("
	case tokenTo:
		return "TO"
	case tokenFn:
		return "FN"
	case tokenSpc:
		return "SPC("
	case tokenThen:
		return "THEN"
	case tokenNot:
		return "NOT"
	case tokenStep:
		return "STEP"
	case tokenPlus:
		return "+"
	case tokenMinus:
		return "-"
	case tokenMul:
		return "*"
	case tokenDiv:
		return "/"
	case tokenPow:
		return "^"
	case tokenAnd:
		return "AND"
	case tokenOr:
		return "OR"
	case tokenGT:
		return ">"
	case tokenEQ:
		return "="
	case tokenLT:
		return "<"
	case tokenSgn:
		return "SGN"
	case tokenInt:
		return "INT"
	case tokenAbs:
		return "ABS"
	case tokenUsr:
		return "USR"
	case tokenFre:
		return "FRE"
	case tokenPos:
		return "POS"
	case tokenSqr:
		return "SQR"
	case tokenRnd:
		return "RND"
	case tokenLog:
		return "LOG"
	case tokenExp:
		return "EXP"
	case tokenCos:
		return "COS"
	case tokenSin:
		return "SIN"
	case tokenTan:
		return "TAN"
	case tokenAtn:
		return "ATN"
	case tokenPeek:
		return "PEEK"
	case tokenLen:
		return "LEN"
	case tokenStr:
		return "STR$"
	case tokenVal:
		return "VAL"
	case tokenAsc:
		return "ASC"
	case tokenChr:
		return "CHR$"
	case tokenLeft:
		return "LEFT$"
	case tokenRight:
		return "RIGHT$"
	case tokenMid:
		return "MID$"
	case tokenGo:
		return "GO"
	case tokenSoundMasterOff:
		return "SM_OFF"
	case tokenSoundMasterIf:
		return "SM_IF"
	case tokenSoundMasterVolume:
		return "SM_VOLUME"
	case tokenSoundMasterWave:
		return "SM_WAVE"
	case tokenSoundMasterEnvelope:
		return "SM_ENVELOPE"
	case tokenSoundMasterOscillate:
		return "SM_OSCILLATE"
	case tokenSoundMasterTune:
		return "SM_TUNE"
	case tokenSoundMasterPlay:
		return "SM_PLAY"
	case tokenSoundMasterFilter:
		return "SM_FILTER"
	case tokenSoundMasterSoundClear:
		return "SM_SOUND_CLEAR"
	case tokenSoundMasterHelp:
		return "SM_HELP"
	default:
		return ""
	}
}
